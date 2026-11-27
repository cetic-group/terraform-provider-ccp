package ccks

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
)

// PollInterval is the delay between two reads while waiting. A variable so
// the tests can shorten it.
var PollInterval = 15 * time.Second

// PlanSettlePolls is the number of reads after a plan change during which a
// cancellation by the platform is watched for (2 minutes at 15 s).
var PlanSettlePolls = 8

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

// ClusterRunning is the version the control plane actually runs:
// `k8s_version_effective` (cetic-cloud-platform#2146) when the API reports it,
// otherwise `k8s_version` — which is the TARGET from the start of an upgrade,
// also when it failed.
func ClusterRunning(cl *client.K8sCluster) string {
	if cl.K8sVersionEffective != nil && *cl.K8sVersionEffective != "" {
		return *cl.K8sVersionEffective
	}
	return cl.K8sVersion
}

// PoolRunning is the version a pool's nodes actually run (effective, else
// the pool's version, else the control plane's when it inherits).
func PoolRunning(p *client.K8sNodePool, clusterRunning string) string {
	if p.K8sVersionEffective != nil && *p.K8sVersionEffective != "" {
		return *p.K8sVersionEffective
	}
	if p.K8sVersion != nil && *p.K8sVersion != "" {
		return *p.K8sVersion
	}
	return clusterRunning
}

func statusCode(err error) int {
	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode
	}
	return 0
}

// WaitClusterSettled waits until no upgrade runs on the cluster and it is
// `active`, or `error` (returned without error: the caller decides — a failed
// upgrade is relaunched). An upgrade PAUSED until the next maintenance window
// is not waited for (it may last days): it is an error naming it.
func WaitClusterSettled(ctx context.Context, c *client.Client, id string, timeout time.Duration) (*client.K8sCluster, error) {
	deadline := time.Now().Add(timeout)
	for {
		cl, err := c.GetK8sCluster(ctx, id)
		if err != nil {
			return nil, err
		}
		op := cl.UpgradeEnCours
		if op != nil && op.Statut == client.K8sUpgradeStatutEnPause {
			return cl, fmt.Errorf("an upgrade of the cluster (%s to %s, %s) is paused until the next "+
				"maintenance window: let it finish or change the maintenance window, then apply again",
				op.Type, op.VersionCible, op.Origine)
		}
		if op == nil && (cl.Status == client.K8sClusterStatusActive || cl.Status == client.K8sClusterStatusError) {
			return cl, nil
		}
		if op != nil && cl.Status == client.K8sClusterStatusError {
			// An operation still "running" on a cluster in error will not end
			// by itself: fail now rather than wait for the timeout.
			return cl, fmt.Errorf("the cluster is in error state with an upgrade (%s to %s) still running: %s",
				op.Type, op.VersionCible, errMsg(cl.ErrorMessage))
		}
		if time.Now().After(deadline) {
			return cl, fmt.Errorf("timeout waiting for the running upgrade of the cluster to end (status %s)", cl.Status)
		}
		if err := sleep(ctx); err != nil {
			return nil, err
		}
	}
}

// WaitClusterActive waits until the cluster is `active` (PUT /oidc and a
// plan change need it; an upgrade paused for its window does not prevent
// them). `error` is an error.
func WaitClusterActive(ctx context.Context, c *client.Client, id string, timeout time.Duration) (*client.K8sCluster, error) {
	deadline := time.Now().Add(timeout)
	for {
		cl, err := c.GetK8sCluster(ctx, id)
		if err != nil {
			return nil, err
		}
		switch cl.Status {
		case client.K8sClusterStatusActive:
			return cl, nil
		case client.K8sClusterStatusError:
			return cl, fmt.Errorf("cluster is in error state: %s", errMsg(cl.ErrorMessage))
		}
		if time.Now().After(deadline) {
			return cl, fmt.Errorf("timeout waiting for the cluster to be active (status %s)", cl.Status)
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
		running := cl.UpgradeEnCours != nil && (opID == "" || cl.UpgradeEnCours.ID == opID)
		if cl.Status == client.K8sClusterStatusError {
			// With the operation still listed as running or not: an error
			// state does not resolve itself, fail now.
			return cl, fmt.Errorf("the upgrade failed, the cluster is in error state: %s. "+
				"Apply again to relaunch it", errMsg(cl.ErrorMessage))
		}
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
			return fmt.Errorf("upgrade to %s failed: %s. Apply again to relaunch it",
				op.VersionCible, errMsg(op.ErrorMessage))
		}
	}
	return nil
}

// lastControlPlaneFailure is the newest control-plane operation of the
// history (type other than `pool`) when it FAILED towards target.
func lastControlPlaneFailure(ctx context.Context, c *client.Client, id, target string) *client.K8sUpgradeOperation {
	hist, err := c.ListK8sClusterUpgrades(ctx, id, 20)
	if err != nil {
		return nil
	}
	for i := range hist { // newest first
		op := hist[i]
		if op.Type == "pool" {
			continue
		}
		if op.Statut == client.K8sUpgradeStatutEchec && Normalize(op.VersionCible) == Normalize(target) {
			return &op
		}
		return nil
	}
	return nil
}

// Result of an Ensure* call.
type Result struct {
	// Warning, when not empty: the configured version is below the running
	// one (never downgraded).
	Warning string
	// Relaunched: a failed upgrade towards the same target was relaunched.
	Relaunched bool
}

// ControlPlaneTimeouts bounds the waits of EnsureControlPlane.
type ControlPlaneTimeouts struct {
	Settle  time.Duration // a running upgrade, before starting
	Upgrade time.Duration // the upgrade itself
}

// EnsureControlPlane brings the control plane to target:
//
//   - waits for a running upgrade to end FIRST, then compares (an automatic
//     patch may have reached or passed the target meanwhile);
//   - a previous upgrade towards target that failed (cluster `error`, or the
//     newest operation `echec`, or the effective version still below a
//     requested target) is RELAUNCHED by sending the same target again
//     (cetic-cloud-platform#2146); an older API that refuses it gives a clear
//     error instead of recording the target as done;
//   - otherwise upgrades when target is above the running version, and
//     never downgrades (Warning).
func EnsureControlPlane(ctx context.Context, c *client.Client, id, target string, t ControlPlaneTimeouts) (*client.K8sCluster, Result, error) {
	var res Result
	cl, err := WaitClusterSettled(ctx, c, id, t.Settle)
	if err != nil {
		return nil, res, err
	}
	running := ClusterRunning(cl)
	requested := Normalize(cl.K8sVersion) == Normalize(target)
	failed := false
	failMsg := errMsg(cl.ErrorMessage)
	switch {
	case requested && cl.Status == client.K8sClusterStatusError:
		failed = true
	case requested && cl.K8sVersionEffective != nil && IsAbove(target, running):
		failed = true // requested, not reached, nothing running: it stopped
	case requested && cl.K8sVersionEffective == nil:
		// Only without k8s_version_effective: with it, the effective version
		// already tells whether the requested target was reached.
		if op := lastControlPlaneFailure(ctx, c, id, target); op != nil {
			failed, failMsg = true, errMsg(op.ErrorMessage)
		}
	}
	if !failed {
		if cl.Status == client.K8sClusterStatusError {
			return cl, res, fmt.Errorf("cluster is in error state: %s", errMsg(cl.ErrorMessage))
		}
		if !IsAbove(target, running) {
			if cmp, ok := Compare(target, running); ok && cmp < 0 {
				res.Warning = fmt.Sprintf("The control plane runs Kubernetes %s, above the configured "+
					"k8s_version %s. The provider never downgrades: the configured value is recorded, "+
					"the cluster is left as is.", running, target)
			}
			return cl, res, nil
		}
	}
	upgraded, err := c.UpgradeK8sClusterVersion(ctx, id, client.K8sUpgradeVersionRequest{K8sVersion: Normalize(target)})
	if err != nil {
		if failed && (statusCode(err) == http.StatusBadRequest || statusCode(err) == http.StatusUnprocessableEntity) {
			return nil, res, fmt.Errorf("the previous upgrade of the control plane to %s failed (%s) and the "+
				"platform refused to relaunch it (%v). Relaunch it from the console, then apply again",
				target, failMsg, err)
		}
		return nil, res, err
	}
	res.Relaunched = failed
	opID := ""
	if upgraded.UpgradeEnCours != nil {
		opID = upgraded.UpgradeEnCours.ID
	}
	final, err := WaitUpgradeDone(ctx, c, id, opID, t.Upgrade)
	if err != nil {
		return final, res, err
	}
	if final.K8sVersionEffective != nil && IsAbove(target, *final.K8sVersionEffective) {
		return final, res, fmt.Errorf("the upgrade ended but the control plane runs %s, below %s",
			*final.K8sVersionEffective, target)
	}
	return final, res, nil
}

// WaitPoolActive waits until the node pool is `active` again (after a scale,
// a labels change or an upgrade). `error` and `deleting` are errors.
func WaitPoolActive(ctx context.Context, c *client.Client, clusterID, poolID string, timeout time.Duration) (*client.K8sNodePool, error) {
	return waitPool(ctx, c, clusterID, poolID, timeout, false)
}

func waitPool(ctx context.Context, c *client.Client, clusterID, poolID string, timeout time.Duration, acceptError bool) (*client.K8sNodePool, error) {
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
			if acceptError {
				return p, nil
			}
			return p, fmt.Errorf("node pool entered error state: %s", errMsg(p.ErrorMessage))
		case "deleting":
			return p, fmt.Errorf("node pool is being deleted")
		}
		if time.Now().After(deadline) {
			return p, fmt.Errorf("timeout waiting for the node pool to be active (status %s)", p.Status)
		}
		if err := sleep(ctx); err != nil {
			return nil, err
		}
	}
}

// upgradeConflictRetries bounds the retries of a pool upgrade answered 409
// while NO other upgrade runs (a refusal that lasts — images guard, pool
// switched off by the scheduler — must surface). A 409 caused by another
// upgrade running (another pool of the same apply, an automatic patch) is
// not counted: it is waited for, within the Settle timeout.
const upgradeConflictRetries = 3

// PoolTimeouts bounds the waits of EnsurePool.
type PoolTimeouts struct {
	Settle  time.Duration // the other upgrades of the cluster, before starting (whole budget)
	Upgrade time.Duration // the upgrade of this pool itself
}

// EnsurePool brings a pool to target through the pool upgrade route (never
// the PATCH), like EnsureControlPlane: waits first, then compares; a failed
// upgrade of the pool towards target (pool `error` on that version, or its
// effective version still below) is relaunched with the same version;
// never downgrades.
func EnsurePool(ctx context.Context, c *client.Client, clusterID, poolID, target string, t PoolTimeouts) (*client.K8sNodePool, Result, error) {
	var res Result
	settleDeadline := time.Now().Add(t.Settle)
	conflicts := 0
	failed := false
	failMsg := ""
	for {
		cl, err := WaitClusterSettled(ctx, c, clusterID, time.Until(settleDeadline))
		if err != nil {
			return nil, res, err
		}
		p, err := waitPool(ctx, c, clusterID, poolID, time.Until(settleDeadline), true)
		if err != nil {
			return nil, res, err
		}
		running := PoolRunning(p, ClusterRunning(cl))
		requested := p.K8sVersion != nil && Normalize(*p.K8sVersion) == Normalize(target)
		failed = requested && (p.Status == "error" ||
			(p.K8sVersionEffective != nil && IsAbove(target, running)))
		failMsg = errMsg(p.ErrorMessage)
		if !failed && p.Status == "error" {
			return p, res, fmt.Errorf("node pool is in error state: %s", failMsg)
		}
		// A cluster in error only accepts the relaunch of THIS pool's failed
		// upgrade (the failure of a pool step puts the cluster in error too);
		// anything else would be answered 409 again and again.
		if cl.Status == client.K8sClusterStatusError && !(failed && p.Status == "error") {
			return p, res, fmt.Errorf("the cluster is in error state (%s): the pool cannot be upgraded "+
				"before the cluster is repaired (a failed control-plane upgrade is relaunched by applying "+
				"its k8s_version again)", errMsg(cl.ErrorMessage))
		}
		if !failed && !IsAbove(target, running) {
			if cmp, ok := Compare(target, running); ok && cmp < 0 {
				res.Warning = fmt.Sprintf("The pool runs Kubernetes %s, above the configured k8s_version %s. "+
					"The provider never downgrades a pool: the configured value is recorded, the pool is left as is.",
					running, target)
			}
			return p, res, nil
		}
		_, err = c.UpgradeK8sNodePool(ctx, clusterID, poolID, client.K8sNodePoolUpgradeRequest{K8sVersion: Normalize(target)})
		if err == nil {
			break
		}
		if failed && (statusCode(err) == http.StatusBadRequest || statusCode(err) == http.StatusUnprocessableEntity) {
			return nil, res, fmt.Errorf("the previous upgrade of the pool to %s failed (%s) and the platform "+
				"refused to relaunch it (%v). Relaunch it from the console, then apply again", target, failMsg, err)
		}
		if !client.IsConflict(err) || time.Now().After(settleDeadline) {
			return nil, res, err
		}
		if now, gerr := c.GetK8sCluster(ctx, clusterID); gerr != nil || now.UpgradeEnCours == nil {
			conflicts++ // not explained by a running upgrade
			if conflicts > upgradeConflictRetries {
				return nil, res, err
			}
		}
		if err := sleep(ctx); err != nil {
			return nil, res, err
		}
	}
	res.Relaunched = failed
	cl, err := c.GetK8sCluster(ctx, clusterID)
	if err != nil {
		return nil, res, err
	}
	opID := ""
	if cl.UpgradeEnCours != nil {
		opID = cl.UpgradeEnCours.ID
	}
	upgradeDeadline := time.Now().Add(t.Upgrade)
	if _, err := WaitUpgradeDone(ctx, c, clusterID, opID, time.Until(upgradeDeadline)); err != nil {
		return nil, res, err
	}
	p, err := WaitPoolActive(ctx, c, clusterID, poolID, time.Until(upgradeDeadline))
	if err != nil {
		return nil, res, err
	}
	if p.K8sVersionEffective != nil && IsAbove(target, *p.K8sVersionEffective) {
		return p, res, fmt.Errorf("the upgrade ended but the pool runs %s, below %s", *p.K8sVersionEffective, target)
	}
	return p, res, nil
}

// WatchPlanChange watches a plan change applied by PATCH: the platform
// applies it asynchronously and may CANCEL it (node IP block that can no
// longer be extended) — the plan then reverts with the reason in
// `error_message`. The change is watched at least PlanSettlePolls reads;
// while the plan read back differs from the requested one WITHOUT a reason
// (not applied yet), the watch goes on, up to timeout.
func WatchPlanChange(ctx context.Context, c *client.Client, id, target string, timeout time.Duration) (*client.K8sCluster, error) {
	deadline := time.Now().Add(timeout)
	for i := 1; ; i++ {
		if err := sleep(ctx); err != nil {
			return nil, err
		}
		cl, err := c.GetK8sCluster(ctx, id)
		if err != nil {
			return nil, err
		}
		if cl.Status == client.K8sClusterStatusError {
			return cl, fmt.Errorf("cluster entered error state during the plan change: %s", errMsg(cl.ErrorMessage))
		}
		pending := cl.Plan != "" && cl.Plan != target
		if pending && cl.ErrorMessage != nil && *cl.ErrorMessage != "" {
			return cl, fmt.Errorf("the platform cancelled the change to plan %q (the cluster is %q): %s",
				target, cl.Plan, *cl.ErrorMessage)
		}
		if !pending && i >= PlanSettlePolls {
			return cl, nil
		}
		if time.Now().After(deadline) {
			if pending {
				return cl, fmt.Errorf("timeout: the cluster still reports plan %q instead of %q", cl.Plan, target)
			}
			return cl, nil
		}
	}
}
