package devices

import "path/filepath"

// RegistryFileName is the registry's file under the daemon's home directory.
const RegistryFileName = "relay-devices.json"

// RegistryPath is the registry file under dir (the daemon's $SMIND_HOME).
func RegistryPath(dir string) string { return filepath.Join(dir, RegistryFileName) }

// Info is a registered device plus whether a session of it is being served
// right now: the shape `smind relay devices ls` and relay.device.list show.
type Info struct {
	Device
	Connected bool `json:"connected"`
}
