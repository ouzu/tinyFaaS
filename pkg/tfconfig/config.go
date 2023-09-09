package tfconfig

import "github.com/google/uuid"

type TFConfig struct {
	ConfigPort          int
	RProxyConfigPort    int
	RProxyListenAddress string
	RProxyBin           string

	COAPPort int
	HTTPPort int
	GRPCPort int

	Backend string
	ID      string

	Mode          string
	Address       string
	ParentAddress string
	RegistryPort  int
}

func DefaultConfig() TFConfig {
	return TFConfig{
		ConfigPort:          8080,
		RProxyConfigPort:    8081,
		RProxyListenAddress: "",
		RProxyBin:           "./rproxy",

		COAPPort: 5683,
		HTTPPort: 8000,
		GRPCPort: 9000,

		Backend: "docker",
		ID:      uuid.NewString(),

		Mode:          "edge",
		Address:       "",
		ParentAddress: "",
		RegistryPort:  8082,
	}
}
