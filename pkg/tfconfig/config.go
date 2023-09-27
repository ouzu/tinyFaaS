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
	Host          string
	ParentAddress string
	RegistryPort  int

	MistifyStrategy string
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
		Host:          "",
		ParentAddress: "",
		RegistryPort:  8082,

		MistifyStrategy: "leastbusy",
	}
}
