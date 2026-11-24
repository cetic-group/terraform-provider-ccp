package ccks

import (
	"context"
	"fmt"
	"time"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
)

// PollInterval is the delay between two reads while waiting. A variable so
// the tests can shorten it.
var PollInterval = 15 * time.Second

func sleep(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(PollInterval):
		return nil
	}
}

func errMsg(p *string) string {
	if p == nil || *p == "" {
		return "unknown"
	}
	return *p
}

// WaitClusterIdle waits until the cluster is `active` with no upgrade
// running, so a new upgrade is accepted (the platform runs one at a time and
// answers 409 otherwise). An upgrade PAUSED until the next maintenance window
// is not waited for (it may last days): it is an error naming it.
func WaitClusterIdle(ctx context.Context, c *client.Client, id string, timeout time.Duration) (*client.K8sCluster, error) {
	deadline := time.Now().Add(timeout)
	for {
		cl, err := c.GetK8sCluster(ctx, id)
		if err != nil {
			return nil, err
		}
		if cl.Status == client.K8sClusterStatusError {
			return cl, fmt.Errorf("cluster is in error state: %s", errMsg(cl.ErrorMessage))
		}
		op := cl.UpgradeEnCours
		if op != nil && op.Statut == client.K8sUpgradeStatutEnPause {
			return cl, fmt.Errorf("an upgrade of the cluster (%s to %s, %s) is paused until the next "+
				"maintenance window: let it finish or change the maintenance window, then apply again",
				op.Type, op.VersionCible, op.Origine)
		}
		if cl.Status == client.K8sClusterStatusActive && op == nil {
			return cl, nil
		}
		if time.Now().After(deadline) {
			return cl, fmt.Errorf("timeout waiting for the cluster to be idle (status %s)", cl.Status)
		}
		if err := sleep(ctx); err != nil {
			return nil, err
		}
	}
}

// WaitUpgradeDone waits for the end of the upgrade operation opID (the one
// the upgrade call returned in `upgrade_en_cours`): the cluster is back to
// `active` and the operation no longer runs. The operation's final status is
// read from the history: `echec` is an error carrying its message.
func WaitUpgradeDone(ctx context.Context, c *client.Client, id, opID string, timeout time.Duration) (*client.K8sCluster, error) {
	deadline := time.Now().Add(timeout)
	for {
		cl, err := c.GetK8sCluster(ctx, id)
		if err != nil {
			return nil, err
		}
		if cl.Status == client.K8sClusterStatusError {
			return cl, fmt.Errorf("cluster entered error state: %s", errMsg(cl.ErrorMessage))
		}
		running := cl.UpgradeEnCours != nil && (opID == "" || cl.UpgradeEnCours.ID == opID)
		if !running && cl.Status == client.K8sClusterStatusActive {
			return cl, operationOutcome(ctx, c, id, opID)
		}
		if time.Now().After(deadline) {
			return cl, fmt.Errorf("upgrade timeout (status %s)", cl.Status)
		}
		if err := sleep(ctx); err != nil {
			return nil, err
		}
	}
}

// operationOutcome returns an error when the history records opID as
// failed. A history that cannot be read is not an error: the cluster is
// active, the versions read back tell the rest.
func operationOutcome(ctx context.Context, c *client.Client, id, opID string) error {
	if opID == "" {
		return nil
	}
	hist, err := c.ListK8sClusterUpgrades(ctx, id, 20)
	if err != nil {
		return nil
	}
	for _, op := range hist {
		if op.ID == opID && op.Statut == client.K8sUpgradeStatutEchec {
			return fmt.Errorf("upgrade to %s failed: %s", op.VersionCible, errMsg(op.ErrorMessage))
		}
	}
	return nil
}

// WaitPoolActive waits until the node pool is `active` again (after a scale,
// a labels change or an upgrade).
func WaitPoolActive(ctx context.Context, c *client.Client, clusterID, poolID string, timeout time.Duration) (*client.K8sNodePool, error) {
	deadline := time.Now().Add(timeout)
	for {
		p, err := c.GetK8sNodePool(ctx, clusterID, poolID)
		if err != nil {
			return nil, err
		}
		switch p.Status {
		case "active":
			return p, nil
		case "error":
			return p, fmt.Errorf("node pool entered error state: %s", errMsg(p.ErrorMessage))
		}
		if time.Now().After(deadline) {
			return p, fmt.Errorf("timeout waiting for the node pool to be active (status %s)", p.Status)
		}
		if err := sleep(ctx); err != nil {
			return nil, err
		}
	}
}

// upgradeConflictRetries bounds the retries of an upgrade answered 409: a
// concurrent upgrade clears up after a wait, a refusal that lasts (images
// guard, pool switched off by the scheduler) must surface.
const upgradeConflictRetries = 3

// UpgradePool runs the upgrade of one pool to target and waits for its end:
// waits for the cluster to be idle and the pool active (one upgrade at a time
// per cluster — several pools of one configuration are upgraded one after the
// other; a call answered 409 because another upgrade started meanwhile is
// retried after the next wait), calls the upgrade route, then waits for the
// operation and the pool.
func UpgradePool(ctx context.Context, c *client.Client, clusterID, poolID, target string, timeout time.Duration) (*client.K8sNodePool, error) {
	deadline := time.Now().Add(timeout)
	for attempt := 0; ; attempt++ {
		if _, err := WaitClusterIdle(ctx, c, clusterID, time.Until(deadline)); err != nil {
			return nil, err
		}
		if _, err := WaitPoolActive(ctx, c, clusterID, poolID, time.Until(deadline)); err != nil {
			return nil, err
		}
		_, err := c.UpgradeK8sNodePool(ctx, clusterID, poolID, client.K8sNodePoolUpgradeRequest{K8sVersion: target})
		if err == nil {
			break
		}
		if !client.IsConflict(err) || attempt >= upgradeConflictRetries || time.Now().After(deadline) {
			return nil, err
		}
		if err := sleep(ctx); err != nil {
			return nil, err
		}
	}
	cl, err := c.GetK8sCluster(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	opID := ""
	if cl.UpgradeEnCours != nil {
		opID = cl.UpgradeEnCours.ID
	}
	if _, err := WaitUpgradeDone(ctx, c, clusterID, opID, time.Until(deadline)); err != nil {
		return nil, err
	}
	return WaitPoolActive(ctx, c, clusterID, poolID, time.Until(deadline))
}
