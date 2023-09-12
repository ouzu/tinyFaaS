package registry

import (
	"fmt"

	pb "github.com/OpenFogStack/tinyFaaS/mistify/registry/node"
	"github.com/OpenFogStack/tinyFaaS/pkg/tfconfig"
)

type RootNode struct {
	BaseNode
}

func NewRootNode(config *tfconfig.TFConfig) *RootNode {
	return &RootNode{
		BaseNode: BaseNode{
			self: pb.NodeAddress{
				Name:           config.ID,
				Address:        fmt.Sprintf("%s:%d", config.Host, config.RegistryPort),
				ManagerAddress: fmt.Sprintf("%s:%d", config.Host, config.ConfigPort),
				ProxyAddress:   fmt.Sprintf("%s:%d", config.Host, config.HTTPPort),
			},
			config: config,
		},
	}
}

func (r *RootNode) Start() {
	r.serve()
}
