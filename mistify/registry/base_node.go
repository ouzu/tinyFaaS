package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
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

// TODO: remove other node types

type BaseNode struct {
	pb.UnimplementedMistifyServer
	self         pb.NodeAddress
	children     []*pb.NodeAddress
	childClients []pb.MistifyClient
	siblings     []*pb.NodeAddress
	mutex        sync.RWMutex
	parent       *pb.NodeAddress
	parentClient pb.MistifyClient
	config       *tfconfig.TFConfig
	registry     map[string]string
}

func (b *BaseNode) Start() {
	go func() {
		time.Sleep(100 * time.Millisecond)
		if b.config.ParentAddress != "" {
			log.Info("connecting to parent node")
			conn, err := grpc.Dial(
				b.config.ParentAddress,
				grpc.WithTransportCredentials(insecure.NewCredentials()),
			)
			if err != nil {
				log.Fatalf("failed to dial: %v", err)
			}
			b.parentClient = pb.NewMistifyClient(conn)

			log.Info("getting info from parent node")
			info, err := b.parentClient.Info(
				context.Background(),
				&pb.Empty{},
			)
			if err != nil {
				log.Fatalf("failed to get info from parent node: %v", err)
			}

			b.parent = info

			log.Info("registering with parent node")
			_, err = b.parentClient.Register(
				context.Background(),
				&b.self,
			)
			if err != nil {
				log.Fatalf("failed to register with parent node: %v", err)
			}
		}
	}()

	b.serve()
}

func (b *BaseNode) serve() {
	log.Infof("starting registry server on port %d", b.config.RegistryPort)

	lis, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", b.config.RegistryPort))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	var opts []grpc.ServerOption
	grpcServer := grpc.NewServer(opts...)
	pb.RegisterMistifyServer(grpcServer, b)
	grpcServer.Serve(lis)
}

func (b *BaseNode) Info(ctx context.Context, in *pb.Empty) (*pb.NodeAddress, error) {
	return &b.self, nil
}

func (b *BaseNode) UpdateSiblingList(ctx context.Context, in *pb.SiblingList) (*pb.Empty, error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	b.siblings = in.Addresses
	log.Infof("new sibling list: %+v", b.siblings)

	return &pb.Empty{}, nil
}

func (b *BaseNode) Register(ctx context.Context, in *pb.NodeAddress) (*pb.Empty, error) {
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

	// TODO: handle duplicates

	b.children = append(b.children, in)

	b.childClients = append(b.childClients, client)

	go func() {
		time.Sleep(5 * time.Second)
		// heartbeat
		for {
			_, err := client.Info(context.Background(), &pb.Empty{})
			if err != nil {
				log.Warnf("failed to get info from child %s: %v", in.Address, err)

				b.mutex.Lock()
				defer b.mutex.Unlock()

				// remove child
				for i, child := range b.children {
					if child.Address == in.Address {
						b.children = append(b.children[:i], b.children[i+1:]...)
						b.childClients = append(b.childClients[:i], b.childClients[i+1:]...)
						break
					}
				}

				// notify other children
				for _, child := range b.childClients {
					log.Infof("notifying child %s of leaving node %s", child, in.Address)
					_, err := child.UpdateSiblingList(
						context.Background(),
						&pb.SiblingList{Addresses: b.children},
					)
					if err != nil {
						log.Infof("failed to notify child of leaving node %s: %v", child, err)
					}
				}

				return
			}

			time.Sleep(5 * time.Second)
		}
	}()

	// log new child list
	log.Infof("new child list: %+v", b.children)

	// notify children
	for _, child := range b.childClients {
		_, err := child.UpdateSiblingList(
			context.Background(),
			&pb.SiblingList{Addresses: b.children},
		)
		if err != nil {
			log.Warnf("failed to notify child of new node %s: %v", child, err)
		}
	}

	return &pb.Empty{}, nil
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

func (b *BaseNode) deployToChild(name string) {
	// TODO: implement adapter mechanism for deployment strategies

	// round robin for now

	child := b.childClients[rand.Intn(len(b.childClients))]

	log.Infof("deploying function %s to child", name)

	if b.registry[name] == "" {
		log.Warnf("function %s not found in registry", name)
		return
	}

	_, err := child.DeployFunction(context.Background(), &pb.Function{
		Name: name,
		Json: b.registry[name],
	})
	if err != nil {
		log.Warnf("failed to notify child of new function %s: %v", child, err)
		// TODO: maybe deploy to other child?
	}
}

func (b *BaseNode) CallFunction(ctx context.Context, in *pb.FunctionCall) (*pb.FunctionCallResponse, error) {
	log.Debugf("have function call: %+v", in)

	if b.config.Mode == "cloud" {
		log.Warnf("got function call in cloud mode")
	}

	node := b.self.ProxyAddress

	if b.config.Mode == "edge" {
		nodes := b.fetchSiblingFunctions()
		log.Debugf("have sibling functions: %+v", nodes)

		functionNodes := nodes[in.FunctionIdentifier]

		if len(functionNodes) == 0 {
			log.Warnf("no sibling functions found for %s", in.FunctionIdentifier)
			log.Infof("escalating call to parent")

			resp, err := b.parentClient.CallFunction(context.Background(), in)
			if err != nil {
				log.Errorf("failed to escalate call: %v", err)
				return nil, fmt.Errorf("failed to escalate call: %v", err)
			}

			return resp, nil
		}

		// TODO: implement adapter mechanism for load balancing strategies

		// round robin for now
		node = functionNodes[rand.Intn(len(functionNodes))]
	}

	go func() {
		if b.config.Mode == "fog" {
			log.Infof("deploying function %s to children", in.FunctionIdentifier)
			b.deployToChild(in.FunctionIdentifier)
		}
	}()

	log.Infof("calling function %s on node %s", in.FunctionIdentifier, node)

	resp, err := b.callProxy(node, in.FunctionIdentifier, []byte(in.Data))

	if err != nil {
		log.Errorf("failed to call function: %v", err)
		// TODO: failure handling
		return nil, fmt.Errorf("failed to call function: %v", err)
	}

	return &pb.FunctionCallResponse{
		Response: string(resp),
	}, nil
}

func (b *BaseNode) fetchSiblingFunctions() map[string][]string {
	functions := make(map[string][]string)

	// TODO: this needs to be in parallel and use a tight timeout

	for _, sibling := range b.siblings {
		conn, err := grpc.Dial(
			sibling.Address,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if err != nil {
			log.Errorf("failed to dial: %v", err)
			return nil
		}

		client := pb.NewMistifyClient(conn)

		list, err := client.GetFunctionList(context.Background(), &pb.Empty{})
		if err != nil {
			log.Errorf("failed to get function list from sibling %s: %v", sibling.Address, err)
			return nil
		}

		for _, name := range list.FunctionNames {
			functions[name] = append(functions[name], sibling.ProxyAddress)
		}
	}

	return functions
}

func (b *BaseNode) callProxy(node string, name string, payload []byte) ([]byte, error) {
	client := http.Client{
		Timeout: 5 * time.Second,
	}

	req, err := http.NewRequest("POST", fmt.Sprintf("http://%s/%s", node, name), strings.NewReader(string(payload)))
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

		_, err := b.parentClient.RegisterFunction(context.Background(), in)
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
	for _, child := range b.childClients {
		_, err := child.DeployFunction(context.Background(), &pb.Function{
			Name: in.Name,
			Json: in.Json,
		})
		if err != nil {
			log.Warnf("failed to notify child of new function %s: %v", child, err)
		}
	}

	return &pb.Empty{}, nil
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
	}

	return &pb.Empty{}, nil
}
