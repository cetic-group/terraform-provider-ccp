package dbpginstance

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cetic-group/terraform-provider-ccp/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Extension settings of a PostgreSQL instance.
//
// The platform validates and normalizes the settings it receives: it adds the
// extensions an allowed extension depends on, and drops `cron_database` when
// `pg_cron` is not preloaded. Terraform requires the state written after an
// apply to match the plan, so these rules are checked at plan time instead
// (ModifyPlan): the practitioner lists dependencies explicitly, and a mistake
// fails the plan with a clear message rather than the apply.

const cronLibrary = "pg_cron"

// extensionsPending is `extensions_status` while settings are not in effect.
const extensionsPending = "pending"

// databaseNameRe is the platform's rule for `cron_database`.
var databaseNameRe = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)

// extensionsApplyTimeout bounds the wait for new settings to be in effect.
// Preloaded libraries restart the instance pod by pod.
var extensionsApplyTimeout = 20 * time.Minute

// extensionsPollInterval is a variable so that tests run without waiting.
var extensionsPollInterval = 10 * time.Second

// stringSet returns the elements of a known, non-null set.
func stringSet(ctx context.Context, s types.Set) ([]string, bool) {
	if s.IsNull() || s.IsUnknown() {
		return nil, false
	}
	out := []string{}
	s.ElementsAs(ctx, &out, false)
	sort.Strings(out)
	return out, true
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// majorOf reads the major version from `engine_version` ("16", "16.4").
// Unknown or unset: the platform default, 16.
func majorOf(v types.String) int {
	if v.IsNull() || v.IsUnknown() {
		return 16
	}
	head := strings.SplitN(strings.TrimSpace(v.ValueString()), ".", 2)[0]
	if n, err := strconv.Atoi(head); err == nil && n > 0 {
		return n
	}
	return 16
}

// extensionProblem is a plan-time error attached to one attribute.
type extensionProblem struct {
	attr string
	msg  string
}

// checkExtensions validates allowed extensions and preloaded libraries
// against the catalog of the instance's major version.
func checkExtensions(cat *client.DbPgExtensionCatalog, allowed []string, allowedKnown bool, preload []string, preloadKnown bool) []extensionProblem {
	var problems []extensionProblem
	byName := map[string]client.DbPgExtension{}
	names := []string{}
	for _, e := range cat.Extensions {
		byName[e.Name] = e
		names = append(names, e.Name)
	}
	sort.Strings(names)

	if allowedKnown {
		for _, name := range allowed {
			ext, ok := byName[name]
			if !ok {
				if contains(cat.DirectExtensions, name) {
					problems = append(problems, extensionProblem{"allowed_extensions", fmt.Sprintf(
						"%q needs no authorization: create it directly in your database with "+
							"CREATE EXTENSION. Remove it from allowed_extensions.", name)})
				} else {
					problems = append(problems, extensionProblem{"allowed_extensions", fmt.Sprintf(
						"%q is not in the extension catalog of PostgreSQL %d. Available: %s.",
						name, cat.Version, strings.Join(names, ", "))})
				}
				continue
			}
			for _, dep := range ext.Requires {
				if !contains(allowed, dep) {
					problems = append(problems, extensionProblem{"allowed_extensions", fmt.Sprintf(
						"%q requires %q: add %q to allowed_extensions.", name, dep, dep)})
				}
			}
			if ext.RequiresPreload && preloadKnown && !contains(preload, name) {
				problems = append(problems, extensionProblem{"preload_libraries", fmt.Sprintf(
					"%q must be preloaded: add %q to preload_libraries.", name, name)})
			}
		}
	}
	if preloadKnown {
		for _, lib := range preload {
			if !contains(cat.PreloadLibraries, lib) {
				problems = append(problems, extensionProblem{"preload_libraries", fmt.Sprintf(
					"%q cannot be preloaded on PostgreSQL %d. Available: %s.",
					lib, cat.Version, strings.Join(cat.PreloadLibraries, ", "))})
			}
		}
	}
	return problems
}

// plannedSetting returns the extension setting the platform will apply. An
// omitted setting (Optional+Computed) is unknown in the plan of a new
// instance: it is not sent, and the instance starts without it, so it is
// checked as empty. On update, an omitted setting already holds the state's
// value (UseStateForUnknown). A configured value that is not known yet
// (it depends on another resource) cannot be checked.
func (r *dbpgResource) plannedSetting(ctx context.Context, req resource.ModifyPlanRequest, state *dbpgResourceModel, attr string, planned types.Set) ([]string, bool) {
	if v, ok := stringSet(ctx, planned); ok {
		return v, true
	}
	if state != nil || !planned.IsUnknown() {
		return nil, false
	}
	var configured types.Set
	if d := req.Config.GetAttribute(ctx, path.Root(attr), &configured); d.HasError() || !configured.IsNull() {
		return nil, false
	}
	return []string{}, true
}

// ModifyPlan checks extension settings before the apply.
func (r *dbpgResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() || r.client == nil {
		return // destroy, or provider not configured yet
	}
	var plan dbpgResourceModel
	if d := req.Plan.Get(ctx, &plan); d.HasError() {
		return
	}
	var state *dbpgResourceModel
	if !req.State.Raw.IsNull() {
		var s dbpgResourceModel
		if d := req.State.Get(ctx, &s); d.HasError() {
			return
		}
		state = &s
	}

	// Settings applied before have not all taken effect: plan the status as
	// unknown, so that the apply goes through Update and waits for them.
	// Otherwise a configuration equal to the state plans nothing, and the
	// settings stay pending without anyone noticing (#1809).
	if state != nil && state.ExtensionsStatus.ValueString() == extensionsPending {
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("extensions_status"), types.StringUnknown())...)
	}

	allowed, allowedKnown := r.plannedSetting(ctx, req, state, "allowed_extensions", plan.AllowedExtensions)
	preload, preloadKnown := r.plannedSetting(ctx, req, state, "preload_libraries", plan.PreloadLibraries)

	if !plan.CronDatabase.IsNull() && !plan.CronDatabase.IsUnknown() {
		if !databaseNameRe.MatchString(plan.CronDatabase.ValueString()) {
			resp.Diagnostics.AddAttributeError(path.Root("cron_database"), "Invalid extension settings",
				"cron_database must be a database name made of lowercase letters, digits and "+
					"underscores, not starting with a digit (63 characters at most).")
		}
		if preloadKnown && !contains(preload, cronLibrary) {
			resp.Diagnostics.AddAttributeError(path.Root("cron_database"), "Invalid extension settings",
				"cron_database only applies when \"pg_cron\" is listed in preload_libraries.")
		}
	}

	changed := state == nil ||
		!plan.AllowedExtensions.Equal(state.AllowedExtensions) ||
		!plan.PreloadLibraries.Equal(state.PreloadLibraries)
	if !changed || (len(allowed) == 0 && len(preload) == 0) {
		return
	}

	major := majorOf(plan.EngineVersion)
	cat, err := r.client.GetDbPgExtensionCatalog(ctx, major)
	if err != nil {
		if client.IsNotFound(err) {
			resp.Diagnostics.AddError("Extensions not available",
				fmt.Sprintf("No extension can be allowed on PostgreSQL %d.", major))
			return
		}
		resp.Diagnostics.AddWarning("Extension settings not checked",
			"The extension catalog could not be read, so the settings are checked at apply time: "+err.Error())
		return
	}
	for _, p := range checkExtensions(cat, allowed, allowedKnown, preload, preloadKnown) {
		resp.Diagnostics.AddAttributeError(path.Root(p.attr), "Invalid extension settings", p.msg)
	}

	if state != nil && preloadKnown && !plan.PreloadLibraries.Equal(state.PreloadLibraries) {
		resp.Diagnostics.AddAttributeWarning(path.Root("preload_libraries"), "Instance restart",
			"Changing preload_libraries restarts the instance, one replica at a time. "+
				"A single-replica instance is briefly unavailable.")
	}
}

// extensionRequest builds the PATCH body. At creation (`state` nil), only
// non-empty settings are sent: an instance starts with none. On update, only
// the settings that changed are sent; an omitted setting is left as is.
func extensionRequest(ctx context.Context, plan dbpgResourceModel, state *dbpgResourceModel) (client.DbPgExtensionsUpdateRequest, bool) {
	var req client.DbPgExtensionsUpdateRequest
	send := false
	if v, ok := stringSet(ctx, plan.AllowedExtensions); ok &&
		((state == nil && len(v) > 0) || (state != nil && !plan.AllowedExtensions.Equal(state.AllowedExtensions))) {
		req.AllowedExtensions = &v
		send = true
	}
	if v, ok := stringSet(ctx, plan.PreloadLibraries); ok &&
		((state == nil && len(v) > 0) || (state != nil && !plan.PreloadLibraries.Equal(state.PreloadLibraries))) {
		req.PreloadLibraries = &v
		send = true
	}
	if !plan.CronDatabase.IsNull() && !plan.CronDatabase.IsUnknown() &&
		(state == nil || !plan.CronDatabase.Equal(state.CronDatabase)) {
		v := plan.CronDatabase.ValueString()
		req.CronDatabase = &v
		send = true
	}
	return req, send
}

// waitExtensionsApplied polls until the instance runs with its settings.
func waitExtensionsApplied(ctx context.Context, c *client.Client, id string) (*client.DbPgInstance, error) {
	deadline := time.Now().Add(extensionsApplyTimeout)
	for {
		inst, err := c.GetDbPg(ctx, id)
		if err != nil {
			return nil, err
		}
		if inst.ExtensionsStatus == "applied" {
			return inst, nil
		}
		if time.Now().After(deadline) {
			msg := "still pending"
			if inst.ErrorMessage != nil && *inst.ErrorMessage != "" {
				msg = *inst.ErrorMessage
			}
			return inst, fmt.Errorf("extension settings not applied after %s: %s", extensionsApplyTimeout, msg)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(extensionsPollInterval):
		}
	}
}

// extensionsUnavailableDetail explains a refusal because extensions cannot be
// enabled on the instance yet. Shared with the create path's error.
const extensionsUnavailableDetail = "Extensions cannot be enabled on this instance yet. " +
	"An instance created in a region where extensions are offered has them enabled from its creation. " +
	"For an instance created earlier, request their activation, then apply again."

// applyExtensionsOnCreate applies the extension settings of a new instance.
//
// A refusal fails the apply, as on update: the instance is created, but not
// as configured. It returns the instance to write in state, read from the
// platform: never the planned settings, so the state never shows extensions
// the instance does not have (#1809). After a refusal, the instance read
// before the PATCH (the platform changed nothing); after a wait that did not
// end, the last instance read: the settings were accepted, and
// `extensions_status = "pending"` says they are not in effect yet, so the
// next plan makes the apply wait for them (ModifyPlan).
//
// Terraform marks a resource whose creation returned an error as tainted:
// the message tells how to keep the instance (`terraform untaint`), after
// which the next plan proposes the extension settings again, in place.
func (r *dbpgResource) applyExtensionsOnCreate(ctx context.Context, plan dbpgResourceModel, inst *client.DbPgInstance, diags *diag.Diagnostics) *client.DbPgInstance {
	req, send := extensionRequest(ctx, plan, nil)
	if !send {
		return inst
	}
	if _, err := r.client.UpdateDbPgExtensions(ctx, inst.ID, req); err != nil {
		if client.IsConflict(err) && !inst.ExtensionsAvailable {
			diags.AddError("PostgreSQL extensions not available on this instance",
				"The PostgreSQL instance was created, but its extension settings were not applied. "+
					extensionsUnavailableDetail+"\n\n"+createdNotAppliedHint)
		} else {
			diags.AddError("Failed to apply PostgreSQL extensions",
				"The PostgreSQL instance was created, but its extension settings were not applied: "+
					err.Error()+"\n\n"+createdNotAppliedHint)
		}
		return inst
	}
	final, err := waitExtensionsApplied(ctx, r.client, inst.ID)
	if err != nil {
		diags.AddError("PostgreSQL extension settings not in effect yet",
			"The PostgreSQL instance was created and its extension settings were accepted, "+
				"but they were not in effect yet: "+err.Error()+"\n\n"+createdPendingHint)
		if final != nil {
			return final
		}
		return inst
	}
	return final
}

// createdPendingHint tells what to do with an instance whose settings were
// accepted at creation but not in effect when the wait ended.
const createdPendingHint = "The instance is saved in the state with the settings the platform reports " +
	"(`extensions_status = \"pending\"` while they are not in effect), and Terraform marks it tainted: " +
	"the next apply would replace it. To keep it, run `terraform untaint` on it; the next apply " +
	"then waits for the settings to be in effect."

// createdNotAppliedHint tells what to do with an instance whose creation
// failed on its extension settings.
const createdNotAppliedHint = "The instance is saved in the state with the settings the platform reports, " +
	"and Terraform marks it tainted: the next apply would replace it. To keep it, run " +
	"`terraform untaint` on it; the next plan then proposes the extension settings again."
