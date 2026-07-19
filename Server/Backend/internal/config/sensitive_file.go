package config

// ProtectSensitiveFile restricts an existing file to the service account and
// platform administrators. Callers should create the file before invoking it.
func ProtectSensitiveFile(path string) error {
	return protectSensitiveFile(path)
}
