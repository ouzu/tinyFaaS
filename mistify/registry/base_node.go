package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/log"

	pb "github.com/OpenFogStack/tinyFaaS/mistify/registry/node"
	"github.com/OpenFogStack/tinyFaaS/pkg/tfconfig"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type RegistryService interface {
	Start()
}

type Function struct {
	Name    string `json:"name"`
	Threads int    `json:"threads"`
	Zip     string `json:"zip"`
	Env     string `json:"env"`
}

type NodeConnection struct {
	Address *pb.NodeAddress
	Client  pb.MistifyClient
}

func NCtoAddr(nc []NodeConnection) []*pb.NodeAddress {
	var addresses []*pb.NodeAddress

	for _, c := range nc {
		addresses = append(addresses, c.Address)
	}

	return addresses
}

func NCtoNames(nc []NodeConnection) []string {
	var names []string

	for _, c := range nc {
		names = append(names, c.Address.Name)
	}

	return names
}

// TODO: remove other node types

type BaseNode struct {
	pb.UnimplementedMistifyServer
	self                     NodeConnection
	children                 []NodeConnection
	siblings                 []NodeConnection
	mutex                    sync.RWMutex
	parent                   NodeConnection
	config                   *tfconfig.TFConfig
	registry                 map[string]string
	selectionContext         StrategyContext
	selectionContextAge      time.Time
	selectionStrategy        NodeSelectionStrategy
	updatingSelectionContext bool
	updatingSelectionMutex   sync.Mutex
	functionDeployments      map[string][]string
	functionDeploymentMutex  sync.RWMutex
}

func (b *BaseNode) updateSelectionContext() {
	b.updatingSelectionMutex.Lock()
	if b.updatingSelectionContext {
		return
	}
	b.updatingSelectionContext = true
	b.updatingSelectionMutex.Unlock()

	defer func() {
		b.updatingSelectionMutex.Lock()
		b.updatingSelectionContext = false
		b.updatingSelectionMutex.Unlock()
	}()

	wg := sync.WaitGroup{}

	functions := make(map[string][]string)
	functionMutex := sync.Mutex{}

	for _, s := range b.siblings {
		wg.Add(1)

		sibling := s

		go func() {
			list, err := sibling.Client.GetFunctionList(context.Background(), &pb.Empty{})
			if err != nil {
				log.Errorf("failed to get function list from sibling %s: %v", sibling.Address, err)
				return
			}

			log.Debug("locking mutex")
			functionMutex.Lock()
			log.Debug("locked mutex")
			functions[sibling.Address.Name] = list.FunctionNames
			functionMutex.Unlock()

			log.Debugf("got function list from sibling %s: %+v", sibling.Address.Name, list.FunctionNames)

			wg.Done()
		}()
	}

	log.Debug("waiting for function list and active requests")

	wg.Wait()

	log.Debug("updating selection context")

	log.Debug("locking mutex")
	b.selectionContext.Mutex.Lock()
	log.Debug("locked mutex")
	defer b.selectionContext.Mutex.Unlock()

	b.selectionContextAge = time.Now()

	b.selectionContext.Siblings = b.siblings
	b.selectionContext.CurrentNode = &b.self
	b.selectionContext.ParentNode = &b.parent
	b.selectionContext.DeployedFuncs = functions

	log.Debug("updated selection context")
}

func (b *BaseNode) Start() {
	go func() {
		for {
			time.Sleep(1000 * time.Millisecond)

			if b.config.ParentAddress != "" {
				log.Debug("connecting to parent node")
				conn, err := grpc.Dial(
					b.config.ParentAddress,
					grpc.WithTransportCredentials(insecure.NewCredentials()),
				)
				if err != nil {
					log.Errorf("failed to dial: %v", err)
					continue
				}
				b.parent.Client = pb.NewMistifyClient(conn)

				log.Debug("getting info from parent node")
				info, err := b.parent.Client.Info(
					context.Background(),
					&pb.Empty{},
				)
				if err != nil {
					log.Errorf("failed to get info from parent node: %v", err)
					continue
				}

				b.parent.Address = info

				log.Infof("registering with parent %s", b.parent.Address.Name)
				_, err = b.parent.Client.Register(
					context.Background(),
					b.self.Address,
				)
				if err != nil {
					log.Errorf("failed to register with parent node: %v", err)
					continue
				}
			}

			break
		}
	}()

	b.serve()
}

func (b *BaseNode) serve() {
	log.Infof("starting node %s", b.self.Address.Name)
	log.Debugf("starting registry server on port %d", b.config.RegistryPort)

	lis, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", b.config.RegistryPort))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	go func() {
		time.Sleep(100 * time.Millisecond)
		// set connection to self
		conn, err := grpc.Dial(
			fmt.Sprintf("localhost:%d", b.config.RegistryPort),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if err != nil {
			log.Fatalf("failed to dial: %v", err)
		}
		log.Debug("locking mutex")
		b.mutex.Lock()
		log.Debug("locked mutex")
		defer b.mutex.Unlock()
		b.self.Client = pb.NewMistifyClient(conn)
	}()

	var opts []grpc.ServerOption
	grpcServer := grpc.NewServer(opts...)
	pb.RegisterMistifyServer(grpcServer, b)
	grpcServer.Serve(lis)
}

func (b *BaseNode) Info(ctx context.Context, in *pb.Empty) (*pb.NodeAddress, error) {
	return b.self.Address, nil
}

func (b *BaseNode) GetFunctionList(ctx context.Context, in *pb.Empty) (*pb.FunctionList, error) {
	managerAddress := fmt.Sprintf("localhost:%d", b.config.ConfigPort)

	// make http call to the list endpoint
	resp, err := http.Get(fmt.Sprintf("http://%s/list", managerAddress))
	if err != nil {
		log.Errorf("failed to get function list: %v", err)
		return nil, fmt.Errorf("failed to get function list: %v", err)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Errorf("failed to read response body: %v", err)
		return nil, fmt.Errorf("failed to read response body: %v", err)
	}

	names := strings.Split(string(body), "\n")

	// remove empty strings
	for i := 0; i < len(names); i++ {
		if names[i] == "" {
			names = append(names[:i], names[i+1:]...)
			i--
		}
	}

	return &pb.FunctionList{
		FunctionNames: names,
	}, nil
}

// ! **********************************
// ! CODE FOR MAINTAINING THE NODE TREE
// ! **********************************

func (b *BaseNode) UpdateSiblingList(ctx context.Context, in *pb.SiblingList) (*pb.Empty, error) {
	log.Debug("locking mutex")
	b.mutex.Lock()
	log.Debug("locked mutex")
	defer b.mutex.Unlock()

	// TODO: query new siblings only

	b.siblings = make([]NodeConnection, len(in.Addresses))
	for i, address := range in.Addresses {
		conn, err := grpc.Dial(
			address.Address,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if err != nil {
			log.Warnf("failed to dial %s: %v", address.Address, err)
		}

		b.siblings[i].Address = address
		b.siblings[i].Client = pb.NewMistifyClient(conn)
	}

	log.Infof("new sibling list: %+v", NCtoNames(b.siblings))

	return &pb.Empty{}, nil
}

func (b *BaseNode) Register(ctx context.Context, in *pb.NodeAddress) (*pb.Empty, error) {
	if b.config.Mode == "edge" {
		log.Warnf("got registration request in edge mode")
		return nil, fmt.Errorf("edge nodes cannot accept registrations")
	}

	log.Debug("locking mutex")
	b.mutex.Lock()
	log.Debug("locked mutex")
	defer b.mutex.Unlock()

	conn, err := grpc.Dial(
		in.Address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to dial: %v", err)
	}

	client := pb.NewMistifyClient(conn)

	_, err = client.Info(context.Background(), &pb.Empty{})
	if err != nil {
		return nil, fmt.Errorf("failed to get info from child %s: %v", in.Address, err)
	}

	// check for duplicates
	for i, child := range b.children {
		if child.Address.Name == in.Name {
			log.Warnf("duplicate child %s", in.Name)
			b.children = append(b.children[:i], b.children[i+1:]...)
			break
		}
	}

	b.children = append(b.children, NodeConnection{
		Address: in,
		Client:  client,
	})

	// log new child list
	log.Infof("new child list: %+v", NCtoNames(b.children))

	// notify children
	for _, child := range b.children {
		_, err := child.Client.UpdateSiblingList(
			context.Background(),
			&pb.SiblingList{Addresses: NCtoAddr(b.children)},
		)
		if err != nil {
			log.Warnf("failed to notify child of new node %s: %v", child, err)
		}
	}

	return &pb.Empty{}, nil
}

// ! ********************************
// ! CODE FOR MANAGING FUNCTION CALLS
// ! ********************************

func (b *BaseNode) CallFunctionLocal(ctx context.Context, in *pb.FunctionCall) (*pb.FunctionCallResponse, error) {
	log.Debugf("have local function call: %+v", in)

	resp, err := b.callProxy(in.FunctionIdentifier, []byte(in.Data))
	if err != nil {
		log.Errorf("failed to call function: %v", err)
		return nil, fmt.Errorf("failed to call function: %v", err)
	}

	return &pb.FunctionCallResponse{
		Response: string(resp),
		Node:     b.self.Address,
	}, nil
}

func (b *BaseNode) CallFunction(ctx context.Context, in *pb.FunctionCall) (*pb.FunctionCallResponse, error) {
	log.Debugf("have function call: %+v", in)

	if b.config.Mode == "cloud" {
		log.Warnf("got function call in cloud mode")
	}

	if b.selectionContextAge == (time.Time{}) {
		log.Debug("updating empty selection context")
		b.updateSelectionContext()
	}

	if time.Since(b.selectionContextAge) > 5*time.Second {
		log.Debug("updating old selection context in background")
		go b.updateSelectionContext()
	}

	if b.config.Mode == "edge" {

		backoff := 1 * time.Second

		requestedDeployment := false

		for {
			result, err := b.selectionStrategy.SelectNode(&b.selectionContext, in.FunctionIdentifier)
			if err != nil {
				log.Errorf("failed to select node: %v", err)
				return nil, fmt.Errorf("failed to select node: %v", err)
			}

			if result.NeedsDeployment && !requestedDeployment {
				requestedDeployment = true
				log.Infof("requesting deployment of function %s on node %s", in.FunctionIdentifier, result.SelectedNode.Address.Name)

				deploy := func() error {
					_, err := b.parent.Client.RequestDeployment(context.Background(), &pb.DeploymentRequest{
						FunctionName: in.FunctionIdentifier,
						TargetNode:   result.DeploymentTarget.Address,
					})

					if err != nil {
						log.Errorf("failed to request deployment: %v", err)
						return fmt.Errorf("failed to request deployment: %v", err)
					}

					return nil
				}

				if err != nil {
					log.Errorf("failed to request deployment: %v", err)
					return nil, fmt.Errorf("failed to request deployment: %v", err)
				}

				if result.SyncDeployment {
					backoff = 1 * time.Second

					for {
						err = deploy()
						if err != nil {
							log.Warnf("failed to request deployment: %v", err)
							if backoff > 40*time.Second {
								log.Errorf("max backoff exceeded, giving up: %v", err)
								return nil, fmt.Errorf("failed to request deployment: %v", err)
							}
							time.Sleep(backoff)
							backoff *= 2
						} else {
							break
						}
					}
				} else {
					go deploy()
				}
			}

			log.Infof("calling function %s on node %s", in.FunctionIdentifier, result.SelectedNode.Address.Name)

			log.Debug("locking mutex")
			b.selectionContext.Mutex.Lock()
			log.Debug("locked mutex")
			b.selectionContext.ActiveRequests[result.SelectedNode.Address.Name]++
			b.selectionContext.Mutex.Unlock()

			resp, err := result.SelectedNode.Client.CallFunctionLocal(context.Background(), in)

			log.Debug("locking mutex")
			b.selectionContext.Mutex.Lock()
			log.Debug("locked mutex")
			b.selectionContext.ActiveRequests[result.SelectedNode.Address.Name]--
			b.selectionContext.Mutex.Unlock()

			if err != nil {
				log.Warnf("failed to call function: %v", err)

				if backoff > 10*time.Second {
					log.Errorf("max backoff exceeded, giving up: %v", err)
					return nil, fmt.Errorf("failed to call function: %v", err)
				}

				time.Sleep(backoff)
				backoff *= 2
				continue
			}

			return &pb.FunctionCallResponse{
				Response: resp.Response,
				Node:     resp.Node,
			}, nil
		}
	}

	backoff := 1 * time.Second

	for {
		log.Infof("calling function %s locally", in.FunctionIdentifier)
		resp, err := b.callProxy(in.FunctionIdentifier, []byte(in.Data))

		if err != nil {
			log.Errorf("failed to call function: %v", err)

			if backoff > 10*time.Second {
				log.Errorf("max backoff exceeded, giving up: %v", err)
				return nil, fmt.Errorf("failed to call function: %v", err)
			}

			time.Sleep(backoff)
			backoff *= 2
			continue
		}

		return &pb.FunctionCallResponse{
			Response: string(resp),
			Node:     b.self.Address,
		}, nil
	}
}

func (b *BaseNode) callProxy(name string, payload []byte) ([]byte, error) {
	client := http.Client{
		Timeout: 5 * time.Second,
	}

	req, err := http.NewRequest("POST", fmt.Sprintf("http://%s/%s", b.self.Address.ProxyAddress, name), strings.NewReader(string(payload)))
	if err != nil {
		log.Errorf("failed to create request: %v", err)
		return nil, fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("X-mistify-bypass", "bypass")

	resp, err := client.Do(req)
	if err != nil {
		log.Errorf("failed to call function: %v", err)
		return nil, fmt.Errorf("failed to call function: %v", err)
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Errorf("failed to read response body: %v", err)
		return nil, fmt.Errorf("failed to read response body: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Warnf("function call failed with status code %d", resp.StatusCode)
		return body, fmt.Errorf("function call failed with status code %d", resp.StatusCode)
	}

	return body, nil
}

// ! *************************************
// ! CODE FOR MANAGING FUNCTION DEPLOYMENT
// ! *************************************
func (b *BaseNode) deployFunction(function *Function) error {
	log.Infof("deploying function %s", function.Name)

	j, err := json.Marshal(function)
	if err != nil {
		log.Errorf("failed to marshal function: %v", err)
		return fmt.Errorf("failed to marshal function: %v", err)
	}

	req, err := http.NewRequest("POST", fmt.Sprintf("http://localhost:%d/upload", b.config.ConfigPort), strings.NewReader(string(j)))
	if err != nil {
		log.Errorf("failed to create request: %v", err)
		return fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("X-mistify-bypass", "bypass")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Errorf("failed to upload function: %v", err)
		return fmt.Errorf("failed to upload function: %v", err)
	}

	if res.StatusCode != http.StatusOK {
		log.Errorf("upload failed with status code %d", res.StatusCode)
		return fmt.Errorf("upload failed with status code %d", res.StatusCode)
	}

	if b.config.Mode == "edge" {
		go b.updateSelectionContext()
	}

	return nil
}

func (b *BaseNode) RegisterFunction(ctx context.Context, in *pb.Function) (*pb.Empty, error) {
	if b.config.Mode != "cloud" {
		// forward to parent
		log.Infof("escalating function registration to parent")

		_, err := b.parent.Client.RegisterFunction(context.Background(), in)
		if err != nil {
			log.Errorf("failed to escalate function registration: %v", err)
			return nil, fmt.Errorf("failed to escalate function registration: %v", err)
		}

		return &pb.Empty{}, nil
	}

	function := &Function{
		Name: in.Name,
	}

	err := json.Unmarshal([]byte(in.Json), function)
	if err != nil {
		log.Errorf("failed to unmarshal function: %v", err)
		return nil, fmt.Errorf("failed to unmarshal function: %v", err)
	}

	err = b.deployFunction(function)
	if err != nil {
		return nil, fmt.Errorf("failed to deploy function: %v", err)
	}

	log.Debug("locking mutex")
	b.mutex.Lock()
	log.Debug("locked mutex")
	b.registry[in.Name] = function.Name
	b.mutex.Unlock()

	// deploy function to fog nodes
	wg := sync.WaitGroup{}

	for _, child := range b.children {
		c := child
		wg.Add(1)
		go func() {
			_, err := c.Client.DeployFunction(context.Background(), &pb.Function{
				Name: in.Name,
				Json: in.Json,
			})
			if err != nil {
				log.Warnf("failed to notify child of new function %s: %v", c.Address.Name, err)
			}
			wg.Done()
		}()
	}

	wg.Wait()

	return &pb.Empty{}, nil
}

func (b *BaseNode) deployToRelevantChildren(name string) {
	// get all children which have the function
	for _, child := range b.children {
		// list functions
		list, err := child.Client.GetFunctionList(context.Background(), &pb.Empty{})
		if err != nil {
			log.Errorf("failed to get function list from child %s: %v", child.Address, err)
			continue
		}

		// check if function is in list
		for _, n := range list.FunctionNames {
			if n == name {
				// deploy function
				_, err := child.Client.DeployFunction(context.Background(), &pb.Function{
					Name: name,
					Json: b.registry[name],
				})
				if err != nil {
					log.Warnf("failed to notify child of updated function %s: %v", child.Address.Name, err)
				}
			}
		}
	}
}

func (b *BaseNode) DeployFunction(ctx context.Context, in *pb.Function) (*pb.Empty, error) {
	if b.config.Mode == "cloud" {
		log.Warnf("got function deployment request in cloud mode")
		return nil, fmt.Errorf("got function deployment request in cloud mode")
	}

	var f Function

	err := json.Unmarshal([]byte(in.Json), &f)
	if err != nil {
		log.Errorf("failed to unmarshal function: %v", err)
		return nil, fmt.Errorf("failed to unmarshal function: %v", err)
	}

	err = b.deployFunction(&f)
	if err != nil {
		return nil, fmt.Errorf("failed to deploy function: %v", err)
	}

	if b.config.Mode == "fog" {
		log.Debug("locking mutex")
		b.mutex.Lock()
		log.Debug("locked mutex")
		b.registry[f.Name] = in.Json
		b.mutex.Unlock()

		go b.deployToRelevantChildren(f.Name)
	}

	// TODO: edge nodes should maybe remove deployted functions when not used for a while

	return &pb.Empty{}, nil
}

func (b *BaseNode) getFunctionDeploymentStatus(name string, child string) bool {
	log.Debug("locking mutex")
	b.functionDeploymentMutex.RLock()
	log.Debug("locked mutex")
	defer b.functionDeploymentMutex.RUnlock()

	if b.functionDeployments == nil {
		return false
	}

	if _, ok := b.functionDeployments[name]; !ok {
		return false
	}

	for _, node := range b.functionDeployments[name] {
		if node == child {
			return true
		}
	}

	return false
}

func (b *BaseNode) setFunctionDeploymentStatus(name string, child string, status bool) {
	log.Debug("locking mutex")
	b.functionDeploymentMutex.Lock()
	log.Debug("locked mutex")
	defer b.functionDeploymentMutex.Unlock()

	if b.functionDeployments == nil {
		b.functionDeployments = make(map[string][]string)
	}

	if _, ok := b.functionDeployments[name]; !ok {
		b.functionDeployments[name] = make([]string, 0)
	}

	if status {
		log.Debugf("setting function deployment status for %s to true", name)
		b.functionDeployments[name] = append(b.functionDeployments[name], child)
	} else {
		log.Debugf("setting function deployment status for %s to false", name)
		for i, node := range b.functionDeployments[name] {
			if node == child {
				b.functionDeployments[name] = append(b.functionDeployments[name][:i], b.functionDeployments[name][i+1:]...)
				break
			}
		}
	}
}

func (b *BaseNode) RequestDeployment(ctx context.Context, in *pb.DeploymentRequest) (*pb.Empty, error) {
	log.Infof("got deployment request for function %s", in.FunctionName)

	if b.config.Mode == "cloud" || b.config.Mode == "edge" {
		log.Warnf("got function deployment request in %s mode", b.config.Mode)
		return nil, fmt.Errorf("got function deployment request in %s mode", b.config.Mode)
	}

	target := in.TargetNode

	// check if target is a child
	for _, child := range b.children {
		if child.Address.Name == target.Name {
			// check if function is already being deployed
			if b.getFunctionDeploymentStatus(in.FunctionName, target.Name) {
				log.Warnf("function %s is already being deployed on node %s", in.FunctionName, target.Name)
				return &pb.Empty{}, nil
			}

			// set function deployment status
			b.setFunctionDeploymentStatus(in.FunctionName, target.Name, true)
			defer b.setFunctionDeploymentStatus(in.FunctionName, target.Name, false)

			log.Infof("deploying function to child %s", target.Name)

			_, err := child.Client.DeployFunction(context.Background(), &pb.Function{
				Name: in.FunctionName,
				Json: b.registry[in.FunctionName],
			})

			if err != nil {
				log.Errorf("failed to deploy function: %v", err)
				return nil, fmt.Errorf("failed to deploy function: %v", err)
			}

			return &pb.Empty{}, nil
		}
	}

	log.Warnf("got deployment request for non-existent child %s", target.Name)
	return nil, fmt.Errorf("got deployment request for non-existent child %s", target.Name)
}
