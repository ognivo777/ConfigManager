package api

import (
	"encoding/json"
	"net"
)

const sockPathPrefix = "unix"

// DialUnix connects to a Unix domain socket at the given filesystem path.
func DialUnix(socket string) (net.Conn, error) {
	return net.Dial(sockPathPrefix, socket)
}

// ListenUnix listens on a Unix domain socket at the given filesystem path.
// On Linux this is net.Listen("unix", socket). The helper keeps a single
// implementation that also compiles elsewhere via the net package.
func ListenUnix(socket string) (net.Listener, error) {
	return net.Listen(sockPathPrefix, socket)
}

func writeJSON(c net.Conn, v any) error {
	enc := json.NewEncoder(c)
	return enc.Encode(v)
}

func readJSON(c net.Conn, v any) error {
	dec := json.NewDecoder(c)
	return dec.Decode(v)
}
