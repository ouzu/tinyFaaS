package registry

import (
	"context"
	"fmt"

	pb "github.com/OpenFogStack/tinyFaaS/mistify/registry/node"
	"github.com/OpenFogStack/tinyFaaS/pkg/tfconfig"
	"github.com/charmbracelet/log"
)

const (
	C_LEAST_BUSY                = "leastbusy"
	C_LOCAL                     = "local"
	C_RANDOM                    = "random"
	C_RANDOM_HOT                = "randomhot"
	C_ROUND_ROBIN               = "roundrobin"
	C_ROUND_ROBIN_HOT           = "roundrobinhot"
	C_LEAST_BUSY_EDGE           = "leastbusyedge"
	C_LEAST_CONNECTIONS         = "leastconnections"
	C_LEAST_CONNECTIONS_OFFLOAD = "leastconnectionsoffload"
)

type EdgeNode struct {
	BaseNode
}

func NewEdgeNode(config *tfconfig.TFConfig) *EdgeNode {
	var strategy NodeSelectionStrategy

	switch config.MistifyStrategy {
	case C_LEAST_CONNECTIONS_OFFLOAD:
		log.Info("Using least connections strategy with offloading")
		strategy = &LeastConnectionsOffloadStrategy{}
	case C_LEAST_CONNECTIONS:
		log.Info("Using least connections strategy")
		strategy = &LeastConnectionsStrategy{}
	case C_LEAST_BUSY:
		log.Info("Using least busy strategy")
		strategy = &LeastBusyStrategy{}
	case C_LEAST_BUSY_EDGE:
		log.Info("Using least busy edge strategy")
		strategy = &LeastBusyEdgeStrategy{}
	case C_LOCAL:
		log.Info("Using local strategy")
		strategy = &LocalOnlyStrategy{}
	case C_RANDOM:
		log.Info("Using random strategy")
		strategy = &RandomStrategy{}
	case C_RANDOM_HOT:
		log.Info("Using random hot strategy")
		strategy = &RandomHotStrategy{}
	case C_ROUND_ROBIN:
		log.Info("Using round robin strategy")
		strategy = &RoundRobinStrategy{}
	case C_ROUND_ROBIN_HOT:
		log.Info("Using round robin hot strategy")
		strategy = &RoundRobinHotStrategy{}
	default:
		log.Info("Invalid strategy, falling back to least busy")
		strategy = &LeastBusyStrategy{}
	}

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
			config:            config,
			selectionStrategy: strategy,
			selectionContext: StrategyContext{
				ActiveRequests: make(map[string]int),
			},
		},
	}
}

func (e *EdgeNode) Register(ctx context.Context, in *pb.NodeAddress) (*pb.Empty, error) {
	return nil, fmt.Errorf("edge nodes do not accept registrations")
}
