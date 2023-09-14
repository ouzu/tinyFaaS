package registry

import (
	"fmt"

	pb "github.com/OpenFogStack/tinyFaaS/mistify/registry/node"
	"github.com/OpenFogStack/tinyFaaS/pkg/tfconfig"
)

type FogNode struct {
	BaseNode
}

func NewFogNode(config *tfconfig.TFConfig) *FogNode {
	return &FogNode{
		BaseNode: BaseNode{
			self: NodeConnection{
				&pb.NodeAddress{
					Name:           config.ID,
					Address:        fmt.Sprintf("%s:%d", config.Host, config.RegistryPort),
					ManagerAddress: fmt.Sprintf("%s:%d", config.Host, config.ConfigPort),
					ProxyAddress:   fmt.Sprintf("%s:%d", config.Host, config.HTTPPort),
				},
				nil,
			},
			config:   config,
			registry: make(map[string]string),
		},
	}
}
