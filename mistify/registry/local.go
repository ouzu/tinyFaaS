package registry

import "github.com/charmbracelet/log"

type LocalOnlyStrategy struct{}

func (l *LocalOnlyStrategy) SelectNode(ctx *StrategyContext, name string) (*NodeSelectionResult, error) {
	log.Debugf("selecting node for function %s", name)

	// Always select the current node
	result := &NodeSelectionResult{
		SelectedNode:     ctx.CurrentNode,
		NeedsDeployment:  true,
		DeploymentTarget: ctx.CurrentNode,
		SyncDeployment:   true,
	}

	// Check if function is deployed on the local node
	if funcs, ok := ctx.DeployedFuncs[ctx.CurrentNode.Address.Name]; ok {
		log.Debugf("local node has the following functions deployed: %v", funcs)
		for _, f := range funcs {
			if f == name {
				log.Debugf("function %s is deployed on the local node", name)
				result.NeedsDeployment = false
				break
			}
		}
	}

	return result, nil
}
