package registry

import (
	"fmt"
	"math/rand"

	"github.com/charmbracelet/log"
)

type LeastConnectionsStrategy struct{}

func (l *LeastConnectionsStrategy) selectDeploymentNode(ctx *StrategyContext, name string) *NodeConnection {
	var coldNodes []NodeConnection

	ctx.Mutex.RLock()
	defer ctx.Mutex.RUnlock()

	for _, node := range ctx.Siblings {
		if funcs, ok := ctx.DeployedFuncs[node.Address.Name]; ok {
			found := false
			for _, f := range funcs {
				if f == name {
					found = true
					break
				}
			}
			if !found {
				coldNodes = append(coldNodes, node)
			}
		} else {
			coldNodes = append(coldNodes, node)
		}
	}

	// check if any nodes do not have the function deployed
	if len(coldNodes) == 0 {
		log.Errorf("no nodes available for deployment of function %s", name)
		return nil
	}

	// get the minimum number of deployed functions
	min := len(ctx.DeployedFuncs[coldNodes[0].Address.Name])
	for _, node := range coldNodes {
		if len(ctx.DeployedFuncs[node.Address.Name]) < min {
			min = len(ctx.DeployedFuncs[node.Address.Name])
		}
	}

	// choose a cold node with the least number of deployed functions
	var minNodes []NodeConnection
	for _, node := range coldNodes {
		if len(ctx.DeployedFuncs[node.Address.Name]) == min {
			minNodes = append(minNodes, node)
		}
	}

	return &minNodes[rand.Intn(len(minNodes))]
}

func (l *LeastConnectionsStrategy) SelectNode(ctx *StrategyContext, name string) (*NodeSelectionResult, error) {
	ctx.Mutex.RLock()
	defer ctx.Mutex.RUnlock()

	log.Debugf("selecting node for function %s", name)

	// build a list of nodes that have the function deployed
	var hotNodes []NodeConnection
	for _, node := range ctx.Siblings {
		if funcs, ok := ctx.DeployedFuncs[node.Address.Name]; ok {
			for _, f := range funcs {
				if f == name {
					hotNodes = append(hotNodes, node)
					break
				}
			}
		}
	}

	// check if any nodes have the function deployed
	if len(hotNodes) == 0 {
		log.Infof("no sibling nodes have function %s deployed", name)

		deploymentTarget := l.selectDeploymentNode(ctx, name)
		if deploymentTarget == nil {
			log.Errorf("no deployment target found for function %s", name)
			return nil, fmt.Errorf("no deployment target found for function %s", name)
		}

		return &NodeSelectionResult{
			SelectedNode:     deploymentTarget,
			NeedsDeployment:  true,
			DeploymentTarget: deploymentTarget,
			SyncDeployment:   true,
		}, nil
	}

	// get the least amount of active requests
	min := ctx.ActiveRequests[hotNodes[0].Address.Name]
	for _, node := range hotNodes {
		if ctx.ActiveRequests[node.Address.Name] < min {
			min = ctx.ActiveRequests[node.Address.Name]
		}
	}

	// choose one among the siblings with the least number of active requests
	var minNodes []NodeConnection
	for _, node := range hotNodes {
		if ctx.ActiveRequests[node.Address.Name] == min {
			minNodes = append(minNodes, node)
		}
	}

	target := minNodes[rand.Intn(len(minNodes))]

	log.Debugf("selecting node %s for function %s", target.Address.Name, name)

	result := &NodeSelectionResult{
		SelectedNode:     &target,
		DeploymentTarget: nil,
		SyncDeployment:   false,
		NeedsDeployment:  false,
	}

	if min > DEPLOYMENT_THRESHOLD {
		log.Infof("deployment threshold exceeded, requesting additional deployment of %s", name)

		deploymentTarget := l.selectDeploymentNode(ctx, name)
		if deploymentTarget == nil {
			log.Warnf("no deployment target found for function %s", name)
			return result, nil
		}

		log.Debugf("selecting node %s for deployment of %s", deploymentTarget.Address.Name, name)

		result.NeedsDeployment = true
		result.DeploymentTarget = deploymentTarget
	}

	return result, nil
}
