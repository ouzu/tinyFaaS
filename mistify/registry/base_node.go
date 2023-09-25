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
	self              NodeConnection
	children          []NodeConnection
	siblings          []NodeConnection
	mutex             sync.RWMutex
	parent            NodeConnection
	config            *tfconfig.TFConfig
	registry          map[string]string
	connectionCount   int
	selectionContext  StrategyContext
	selectionStrategy NodeSelectionStrategy
}

func (b *BaseNode) updateSelectionContext() {
	wg := sync.WaitGroup{}

	functions := make(map[string][]string)
	functionMutex := sync.Mutex{}

	requests := make(map[string]int)
	requestMutex := sync.Mutex{}

	for _, s := range b.siblings {
		wg.Add(2)

		sibling := s

		go func() {
			defer wg.Done()
			list, err := sibling.Client.GetFunctionList(context.Background(), &pb.Empty{})
			if err != nil {
				log.Errorf("failed to get function list from sibling %s: %v", sibling.Address, err)
				return
			}

			functionMutex.Lock()
			functions[sibling.Address.Name] = list.FunctionNames
			functionMutex.Unlock()

			log.Debugf("got function list from sibling %s: %+v", sibling.Address.Name, list.FunctionNames)
		}()

		go func() {
			defer wg.Done()
			count, err := sibling.Client.GetActiveRequests(context.Background(), &pb.Empty{})
			if err != nil {
				log.Errorf("failed to get active requests from sibling %s: %v", sibling.Address, err)
				return
			}

			requestMutex.Lock()
			requests[sibling.Address.Name] = int(count.Count)

			if sibling.Address.Name == b.self.Address.Name {
				// substract the current request
				requests[sibling.Address.Name]--
			}
			requestMutex.Unlock()

			log.Debugf("got active requests from sibling %s: %d", sibling.Address.Name, count.Count)
		}()
	}

	wg.Wait()

	b.mutex.Lock()
	defer b.mutex.Unlock()

	b.selectionContext.Siblings = b.siblings
	b.selectionContext.CurrentNode = &b.self
	b.selectionContext.ParentNode = &b.parent
	b.selectionContext.ActiveRequests = requests
	b.selectionContext.DeployedFuncs = functions
}

func (b *BaseNode) increaseConnectionCount() {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	b.connectionCount++

	log.Debugf("connection count increased to %d", b.connectionCount)
}

func (b *BaseNode) decreaseConnectionCount() {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	b.connectionCount--

	log.Debugf("connection count decreased to %d", b.connectionCount)
}

func (b *BaseNode) Start() {
	go func() {
		time.Sleep(100 * time.Millisecond)
		if b.config.ParentAddress != "" {
			log.Debug("connecting to parent node")
			conn, err := grpc.Dial(
				b.config.ParentAddress,
				grpc.WithTransportCredentials(insecure.NewCredentials()),
			)
			if err != nil {
				log.Fatalf("failed to dial: %v", err)
			}
			b.parent.Client = pb.NewMistifyClient(conn)

			log.Debug("getting info from parent node")
			info, err := b.parent.Client.Info(
				context.Background(),
				&pb.Empty{},
			)
			if err != nil {
				log.Fatalf("failed to get info from parent node: %v", err)
			}

			b.parent.Address = info

			log.Infof("registering with parent %s", b.parent.Address.Name)
			_, err = b.parent.Client.Register(
				context.Background(),
				b.self.Address,
			)
			if err != nil {
				log.Fatalf("failed to register with parent node: %v", err)
			}
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
		b.mutex.Lock()
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

func (b *BaseNode) GetActiveRequests(ctx context.Context, in *pb.Empty) (*pb.RequestCount, error) {
	b.mutex.RLock()
	defer b.mutex.RUnlock()

	return &pb.RequestCount{
		Count: int32(b.connectionCount),
	}, nil
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
	b.mutex.Lock()
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

	b.mutex.Lock()
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

	go b.increaseConnectionCount()
	defer b.decreaseConnectionCount()

	resp, err := b.callProxy(in.FunctionIdentifier, []byte(in.Data))
	if err != nil {
		log.Errorf("failed to call function: %v", err)
		return nil, fmt.Errorf("failed to call function: %v", err)
	}

	return &pb.FunctionCallResponse{
		Response: string(resp),
	}, nil
}

func (b *BaseNode) CallFunction(ctx context.Context, in *pb.FunctionCall) (*pb.FunctionCallResponse, error) {
	log.Debugf("have function call: %+v", in)

	if b.config.Mode == "cloud" {
		log.Warnf("got function call in cloud mode")
	}

	// TODO: cache this
	b.updateSelectionContext()

	if b.config.Mode == "edge" {
		result, err := b.selectionStrategy.SelectNode(&b.selectionContext, in.FunctionIdentifier)
		if err != nil {
			log.Errorf("failed to select node: %v", err)
			return nil, fmt.Errorf("failed to select node: %v", err)
		}

		if result.NeedsDeployment {
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
				err = deploy()
				if err != nil {
					log.Errorf("failed to request deployment: %v", err)
					return nil, fmt.Errorf("failed to request deployment: %v", err)
				}
			} else {
				go deploy()
			}
		}

		log.Infof("calling function %s on node %s", in.FunctionIdentifier, result.SelectedNode.Address.Name)

		resp, err := result.SelectedNode.Client.CallFunctionLocal(context.Background(), in)

		if err != nil {
			log.Errorf("failed to call function: %v", err)

			// TODO: retry
		}

		return &pb.FunctionCallResponse{
			Response: resp.Response,
		}, nil
	}

	log.Infof("calling function %s locally", in.FunctionIdentifier)
	resp, err := b.callProxy(in.FunctionIdentifier, []byte(in.Data))

	if err != nil {
		log.Errorf("failed to call function: %v", err)
		return nil, fmt.Errorf("failed to call function: %v", err)
	}

	return &pb.FunctionCallResponse{
		Response: string(resp),
	}, nil
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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Errorf("failed to read response body: %v", err)
		return nil, fmt.Errorf("failed to read response body: %v", err)
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

	b.mutex.Lock()
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
		b.mutex.Lock()
		b.registry[f.Name] = in.Json
		b.mutex.Unlock()

		go b.deployToRelevantChildren(f.Name)
	}

	// TODO: edge nodes should maybe remove deployted functions when not used for a while

	return &pb.Empty{}, nil
}

func (b *BaseNode) RequestDeployment(ctx context.Context, in *pb.DeploymentRequest) (*pb.Empty, error) {
	log.Infof("got deployment request for function %s", in.FunctionName)

	if b.config.Mode == "cloud" {
		log.Warnf("got function deployment request in cloud mode")
		return nil, fmt.Errorf("got function deployment request in cloud mode")
	}

	target := in.TargetNode

	// check if target is a child
	for _, child := range b.children {
		if child.Address.Name == target.Name {
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
