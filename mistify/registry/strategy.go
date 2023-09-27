package registry

import "sync"

type StrategyContext struct {
	Siblings    []NodeConnection
	CurrentNode *NodeConnection
	ParentNode  *NodeConnection

	// number of active requests for each sibling
	ActiveRequests map[string]int

	// list of deployed functions for each sibling
	DeployedFuncs map[string][]string

	Mutex sync.RWMutex
}

type NodeSelectionResult struct {
	SelectedNode     *NodeConnection
	NeedsDeployment  bool
	DeploymentTarget *NodeConnection
	SyncDeployment   bool
}

type NodeSelectionStrategy interface {
	SelectNode(ctx *StrategyContext, name string) (*NodeSelectionResult, error)
}
