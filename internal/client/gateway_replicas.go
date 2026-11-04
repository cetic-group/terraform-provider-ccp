package client

// GatewayReplicaCount preserves the historical two-instance mode when reading
// a platform version that predates replica_count. An explicit value is never
// replaced with a guessed default.
func GatewayReplicaCount(value *int64) int64 {
	if value == nil {
		return 2
	}
	return *value
}
