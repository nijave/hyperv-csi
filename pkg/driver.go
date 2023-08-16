package pkg

import (
	"context"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog/v2"
	"os"
	"path/filepath"
)

const hypervScsiControllerMax = 64
const hypervScsiControllerReserved = 4
const hypervScsiControllerAvailable = hypervScsiControllerMax - hypervScsiControllerReserved

type HypervCsiDriver struct {
	csi.NodeServer
}

func (s *HypervCsiDriver) NodeGetInfo(ctx context.Context, req *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {
	logRequest("NodeGetInfo", req)
	return &csi.NodeGetInfoResponse{
		NodeId:             os.Getenv("KUBE_NODE_NAME"),
		MaxVolumesPerNode:  hypervScsiControllerAvailable,
		AccessibleTopology: nil,
	}, nil
}

func (s *HypervCsiDriver) NodeGetCapabilities(ctx context.Context, req *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	logRequest("NodeGetCapabilities", req)
	// TODO... I don't think I support any of the listed items...
	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: []*csi.NodeServiceCapability{},
	}, nil
}

// NodePublishVolume Mount a volume to the target path
func (s *HypervCsiDriver) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	logRequest("NodePublishVolume", req)

	// Create a sentinel file
	err := os.MkdirAll(req.TargetPath, 0400)
	if err != nil {
		klog.Warningf("failed to create mount directory %v", err)
	}

	f, err := os.Create(filepath.Join(req.TargetPath, "mount.txt"))
	f.Close()
	if err != nil {
		klog.Warningf("error creating fake mount file %v", err)
	}

	//out, err := exec.Command("/usr/bin/ls", "-Rl", "/var/lib/kubelet/pods").Output()
	//if err != nil {
	//	klog.Warningf("failed to run command %v", err)
	//} else {
	//	klog.Infof("directory structure %s", out)
	//}

	return &csi.NodePublishVolumeResponse{}, nil
}

// NodeUnpublishVolume Unmount a volume from the target path
func (s *HypervCsiDriver) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	logRequest("NodeUnpublishVolume", req)

	err := os.Remove(filepath.Join(req.TargetPath, "mount.txt"))
	if err != nil {
		klog.Warningf("failed to remove fake mount file %v", err)
	}

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

// NodeStageVolume Not supported capability
func (s *HypervCsiDriver) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	logRequest("NodeStageVolume", req)
	return nil, status.Errorf(codes.Unimplemented, "method NodeStageVolume not implemented")
}

// NodeUnstageVolume Not supported capability
func (s *HypervCsiDriver) NodeUnstageVolume(ctx context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	logRequest("NodeUnstageVolume", req)
	return nil, status.Errorf(codes.Unimplemented, "method NodeUnstageVolume not implemented")
}

// NodeGetVolumeStats Not supported capability
func (s *HypervCsiDriver) NodeGetVolumeStats(ctx context.Context, req *csi.NodeGetVolumeStatsRequest) (*csi.NodeGetVolumeStatsResponse, error) {
	logRequest("NodeGetVolumeStats", req)
	return nil, status.Errorf(codes.Unimplemented, "method NodeGetVolumeStats not implemented")
}

// NodeExpandVolume Not supported capability
func (s *HypervCsiDriver) NodeExpandVolume(ctx context.Context, req *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	logRequest("NodeExpandVolume", req)
	return nil, status.Errorf(codes.Unimplemented, "method NodeExpandVolume not implemented")
}
