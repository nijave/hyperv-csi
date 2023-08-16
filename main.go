package main

import (
	"bytes"
	"flag"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/masterzen/winrm"
	"github.com/nijave/hyperv-csi/pkg"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"k8s.io/klog/v2"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

func createWinrmClient(caFilePath *string) *winrm.Client {
	parsed, err := url.Parse(os.Getenv("WINRM_HOST"))
	if err != nil {
		klog.Error(err)
		klog.Warning("couldn't parse WINRM_HOST environment variable")
	}

	var port int
	if parsed.Port() != "" {
		port, err = strconv.Atoi(parsed.Port())
		if err != nil {
			klog.Fatalf("couldn't parse port from WINRM_HOST: %v", err)
		}
	} else {
		port = 5985
	}

	var caCert []byte
	if caFilePath != nil {
		klog.InfoS("using non-default ca file", "cacert", *caFilePath)
		caCert, err = os.ReadFile(*caFilePath)
		if err != nil {
			klog.Fatalf("couldn't read ca file %v", err)
		}
	}

	endpoint := winrm.NewEndpoint(parsed.Hostname(), port, parsed.Scheme == "https", false, caCert, nil, nil, 0)
	params := winrm.DefaultParameters
	params.TransportDecorator = func() winrm.Transporter { return &winrm.ClientNTLM{} }
	winrmClient, err := winrm.NewClientWithParameters(endpoint, os.Getenv("WINRM_USER"), os.Getenv("WINRM_PASSWORD"), params)
	if err != nil {
		klog.Error(err)
		klog.Fatalf("could not create winrm client for %s", endpoint.Host)
	}

	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	output := new(bytes.Buffer)
	exit, err := winrmClient.RunWithContext(ctx, "echo ok", output, output)

	if exit != 0 || err != nil {
		klog.Warning(output.String())
		klog.Fatalf("failed exit %d, %v", exit, err)
	} else {
		klog.InfoS("winrm check", "status", strings.Trim(output.String(), " \n\r\t"))
	}

	return winrmClient
}

func initController(grpcServer *grpc.Server) {
	var caFilePath *string
	if caFilePathOverride := os.Getenv("WINRM_CA_FILE_PATH"); len(caFilePathOverride) > 0 {
		caFilePath = &caFilePathOverride
	}

	// TODO put this in a constant or something
	volumePath := "V:\\Hyper-V\\Virtual Hard Disks"
	if newVolumePath := os.Getenv("HV_VOLUME_PATH"); len(newVolumePath) > 0 {
		volumePath = newVolumePath
	}
	hypervCsiController := &pkg.HypervCsiController{
		WinrmClient: createWinrmClient(caFilePath),
		VolumePath:  volumePath,
	}

	csi.RegisterControllerServer(grpcServer, hypervCsiController)
	csi.RegisterIdentityServer(grpcServer, hypervCsiController)
}

func initDriver(grpcServer *grpc.Server) {
	hypervCsiController := &pkg.HypervCsiController{}
	csi.RegisterIdentityServer(grpcServer, hypervCsiController)
	hypervCsiDriver := &pkg.HypervCsiDriver{}
	csi.RegisterNodeServer(grpcServer, hypervCsiDriver)
}

func main() {
	var grpcService string
	klog.InitFlags(nil)
	flag.StringVar(&grpcService, "grpc-service", "controller", "Which gRPC services should run")
	flag.Parse()

	socket := "/run/csi/socket"
	if envSocket := os.Getenv("CSI_ADDRESS"); len(envSocket) > 0 {
		socket = envSocket
		klog.InfoS("using non-default socket", "socket", socket)
	}

	err := os.Remove(socket)
	if err != nil {
		klog.Infof("error removing existing socket %v", err)
	}

	listen, err := net.Listen("unix", socket)
	if err != nil {
		klog.Fatalf("failed to listen: %v", err)
	}
	defer listen.Close()
	grpcServer := grpc.NewServer()

	switch grpcService {
	case "controller":
		initController(grpcServer)
	case "driver":
		initDriver(grpcServer)
	default:
		listen.Close()
		klog.Fatal("invalid grpc-service specified")
	}

	klog.Infof("server %s listening at %v", grpcService, listen.Addr())
	if err := grpcServer.Serve(listen); err != nil {
		klog.Fatalf("failed to serve: %v", err)
	}
}
