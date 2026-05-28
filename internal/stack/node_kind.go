package stack

import "strings"

const (
	NodeKindHelm                      = "release.helm"
	NodeKindAction                    = "action.script"
	NodeKindActionPlugin              = "action.plugin"
	NodeKindModuleResource            = "module.resource"
	NodeKindDBRestorePoint            = "db.restore-point"
	NodeKindDBSchemaExpand            = "db.schema-expand"
	NodeKindDBBackfill                = "db.backfill"
	NodeKindDBVerify                  = "db.verify"
	NodeKindDBCutover                 = "db.cutover"
	NodeKindDBSchemaContract          = "db.schema-contract"
	NodeKindMySQLReplicationVerify    = "mysql.replication.verify"
	NodeKindPostgresRoleEnsure        = "postgres.role.ensure"
	NodeKindPostgresDatabaseEnsure    = "postgres.database.ensure"
	NodeKindPostgresGrantEnsure       = "postgres.grant.ensure"
	NodeKindPostgresSchemaEnsure      = "postgres.schema.ensure"
	NodeKindPostgresExtensionEnsure   = "postgres.extension.ensure"
	NodeKindPostgresReplicationVerify = "postgres.replication.verify"
	NodeKindPostgresBackupRun         = "postgres.backup.run"
	NodeKindPostgresBackupVerify      = "postgres.backup.verify"
	NodeKindPostgresRestoreDrill      = "postgres.restore.drill"
	NodeKindPostgresConfigEnsure      = "postgres.config.ensure"
	NodeKindPostgresMaintenanceRun    = "postgres.maintenance.run"
	NodeKindHostCommandRun            = "host.command.run"
	NodeKindHostFileRender            = "host.file.render"
	NodeKindHostFileCopy              = "host.file.copy"
	NodeKindHostPackageInstall        = "host.package.install"
	NodeKindHostServiceManage         = "host.service.manage"
	NodeKindHostUserManage            = "host.user.manage"
	NodeKindHostCronManage            = "host.cron.manage"
	NodeKindHostSystemdUnit           = "host.systemd.unit"
	NodeKindK8sClusterInspect         = "k8s.cluster.inspect"
	NodeKindK8sManifestApply          = "k8s.manifest.apply"
	NodeKindK8sManifestDelete         = "k8s.manifest.delete"
	NodeKindK8sResourceWait           = "k8s.resource.wait"
	NodeKindK8sLogsCapture            = "k8s.logs.capture"
	NodeKindK8sEventsCapture          = "k8s.events.capture"
	NodeKindK8sCertInspect            = "k8s.cert.inspect"
	NodeKindK8sCertRenew              = "k8s.cert.renew"
	NodeKindK8sClusterVerify          = "k8s.cluster.verify"
)

func normalizeNodeKind(kind string) string {
	kind = strings.TrimSpace(strings.ToLower(kind))
	if kind == "" || kind == "release" || kind == "helm" {
		return NodeKindHelm
	}
	return kind
}

func isHelmNode(n *ResolvedRelease) bool {
	if n == nil {
		return false
	}
	return normalizeNodeKind(n.Kind) == NodeKindHelm
}

func isPostgresNodeKind(kind string) bool {
	switch normalizeNodeKind(kind) {
	case NodeKindPostgresRoleEnsure,
		NodeKindPostgresDatabaseEnsure,
		NodeKindPostgresGrantEnsure,
		NodeKindPostgresSchemaEnsure,
		NodeKindPostgresExtensionEnsure,
		NodeKindPostgresReplicationVerify,
		NodeKindPostgresBackupRun,
		NodeKindPostgresBackupVerify,
		NodeKindPostgresRestoreDrill,
		NodeKindPostgresConfigEnsure,
		NodeKindPostgresMaintenanceRun:
		return true
	default:
		return false
	}
}

func nodeStepSucceeded(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "success", "succeeded", "skipped":
		return true
	default:
		return false
	}
}
