package registry

import (
	"context"
	"fmt"

	pb "github.com/OpenFogStack/tinyFaaS/mistify/registry/node"
	"github.com/OpenFogStack/tinyFaaS/pkg/tfconfig"
)

type EdgeNode struct {
	BaseNode
}

func NewEdgeNode(config *tfconfig.TFConfig) *EdgeNode {
	return &EdgeNode{
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
			config: config,
		},
	}
}

func (e *EdgeNode) Register(ctx context.Context, in *pb.NodeAddress) (*pb.Empty, error) {
	return nil, fmt.Errorf("edge nodes do not accept registrations")
}
