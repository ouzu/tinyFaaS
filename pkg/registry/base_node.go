package registry

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	pb "github.com/OpenFogStack/tinyFaaS/pkg/registry/node"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type RegistryService interface {
	Start()
}

type BaseNode struct {
	pb.UnimplementedTFRegistryServer
	self         pb.NodeAddress
	children     []*pb.NodeAddress
	childClients []pb.TFRegistryClient
	siblings     []*pb.NodeAddress
	mutex        sync.RWMutex
	parent       *pb.NodeAddress
	parentAddr   string
	parentClient pb.TFRegistryClient
	port         int
}

func (b *BaseNode) Start() {
	go b.serve()

	if b.parentAddr != "" {
		log.Println("connecting to parent node")
		conn, err := grpc.Dial(
			b.parentAddr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if err != nil {
			log.Fatalf("failed to dial: %v", err)
		}
		b.parentClient = pb.NewTFRegistryClient(conn)

		log.Println("getting info from parent node")
		info, err := b.parentClient.Info(
			context.Background(),
			&pb.Empty{},
		)
		if err != nil {
			log.Fatalf("failed to get info from parent node: %v", err)
		}

		b.parent = info

		log.Println("registering with parent node")
		_, err = b.parentClient.Register(
			context.Background(),
			&pb.NodeAddress{
				Address: b.self.Address,
				Name:    b.self.Name,
			},
		)
		if err != nil {
			log.Fatalf("failed to register with parent node: %v", err)
		}
	}
}

func (b *BaseNode) serve() {
	log.Printf("starting registry server on port %d", b.port)

	lis, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", b.port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	var opts []grpc.ServerOption
	grpcServer := grpc.NewServer(opts...)
	pb.RegisterTFRegistryServer(grpcServer, b)
	grpcServer.Serve(lis)
}

func (b *BaseNode) Info(ctx context.Context, in *pb.Empty) (*pb.NodeAddress, error) {
	return &pb.NodeAddress{
		Address: b.self.Address,
		Name:    b.self.Name,
	}, nil
}

func (b *BaseNode) UpdateSiblingList(ctx context.Context, in *pb.SiblingList) (*pb.Empty, error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	b.siblings = in.Addresses
	log.Printf("new sibling list: %+v", b.siblings)

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

	client := pb.NewTFRegistryClient(conn)

	_, err = client.Info(context.Background(), &pb.Empty{})
	if err != nil {
		return nil, fmt.Errorf("failed to get info from child %s: %v", in.Address, err)
	}

	b.children = append(b.children, &pb.NodeAddress{
		Address: in.Address,
		Name:    in.Name,
	})

	b.childClients = append(b.childClients, client)

	go func() {
		time.Sleep(5 * time.Second)
		// heartbeat
		for {
			_, err := client.Info(context.Background(), &pb.Empty{})
			if err != nil {
				log.Printf("failed to get info from child %s: %v", in.Address, err)

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
					log.Printf("notifying child %s of leaving node %s", child, in.Address)
					_, err := child.UpdateSiblingList(
						context.Background(),
						&pb.SiblingList{Addresses: b.children},
					)
					if err != nil {
						log.Printf("failed to notify child of leaving node %s: %v", child, err)
					}
				}

				return
			}

			time.Sleep(5 * time.Second)
		}
	}()

	// log new child list
	log.Printf("new child list: %+v", b.children)

	// notify children
	for _, child := range b.childClients {
		_, err := child.UpdateSiblingList(
			context.Background(),
			&pb.SiblingList{Addresses: b.children},
		)
		if err != nil {
			log.Printf("failed to notify child of new node %s: %v", child, err)
		}
	}

	return &pb.Empty{}, nil
}
