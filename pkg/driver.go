package pkg

import (
	"context"
	"fmt"
	"github.com/bitfield/script"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog/v2"
	"os"
	"os/exec"
	"strings"
)

const hypervScsiControllerMax = 64
const hypervScsiControllerReserved = 4
const hypervScsiControllerAvailable = hypervScsiControllerMax - hypervScsiControllerReserved
const defaultFilesystem = "ext4"

//const hostFilesystemMountPoint = "/host"

func volumeDeviceSuffix(volumeId string) string {
	return volumeId[strings.LastIndex(volumeId, "-")+1:]
}

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

	response := &csi.NodePublishVolumeResponse{}

	// Determine filesystem type
	fsType := defaultFilesystem
	if req.GetVolumeCapability() != nil && req.GetVolumeCapability().GetMount() != nil && req.GetVolumeCapability().GetMount().GetFsType() != "" {
		fsType = req.GetVolumeCapability().GetMount().GetFsType()
	}
	klog.Infof("using fstype %s", fsType)

	// Find block device from pvc ID (vhd id)
	// TODO probably convert this to not use bitfield/script since that's the only place the dep is used
	volumePath, err := script.ListFiles(fmt.Sprintf("/dev/disk/by-id/wwn-*%s", volumeDeviceSuffix(req.VolumeId))).First(1).String()
	volumePath = strings.TrimRight(volumePath, " \t\n\r")
	if err != nil {
		klog.ErrorS(err, "Couldn't find device for volume", "volumeId", req.VolumeId, "output", volumePath)
		return response, err
	}

	// Partition block device, if needed
	partitionPath := volumePath + "-part1"
	if _, err = os.Stat(partitionPath); err != nil {
		klog.InfoS("partitioning pv", "pv", req.VolumeId)
		shellCommand := []string{volumePath, "--script", "-a", "optimal", "mklabel", "gpt", "mkpart", "primary", fsType, "0%", "100%"}
		if out, partErr := exec.CommandContext(ctx, "parted", shellCommand...).Output(); partErr != nil {
			klog.ErrorS(partErr, "failed to partition disk", "command", shellCommand, "output", string(out))
			return response, partErr
		}
	}

	// Format block device, if needed
	out, err := exec.CommandContext(ctx, "blkid", "-o", "value", "-s", "TYPE", partitionPath).Output()
	if err != nil {
		klog.ErrorS(err, "couldn't determine partition fstype", "partition", partitionPath, "output", out)
		return response, err
	}
	if len(out) == 0 {
		klog.InfoS("formatting pv", "pv", req.VolumeId, "fsType", fsType)
		out, err := exec.CommandContext(ctx, "mkfs", "-t", fsType, partitionPath).Output()
		if err != nil {
			klog.ErrorS(err, "couldn't format partition", "fsType", fsType, "partition", partitionPath, "output", out)
			return response, err
		}
	}

	klog.InfoS("creating mount point directory", "directory", req.TargetPath)
	err = os.MkdirAll(req.TargetPath, 0700)

	// Construct mount command
	mountCommand := make([]string, 0)
	var mountFlags []string
	if req.GetVolumeCapability() != nil && req.GetVolumeCapability().GetMount() != nil {
		mountFlags = req.GetVolumeCapability().GetMount().GetMountFlags()
	}
	if len(mountFlags) > 0 {
		// TODO, I think this works right... (need to verify what's actually in mount flags array)
		mountCommand = append(mountCommand, "-o")
		mountCommand = append(mountCommand, strings.Join(mountFlags, ","))
	}
	mountCommand = append(mountCommand, partitionPath)
	mountCommand = append(mountCommand, req.TargetPath)

	// Mount partition
	klog.InfoS("running command", "command", mountCommand)
	out, err = exec.CommandContext(ctx, "mount", mountCommand...).Output()
	// TODO idempotence see https://github.com/container-storage-interface/spec/blob/master/spec.md#nodepublishvolume-errors
	if err != nil {
		klog.ErrorS(err, "failed to mount volume", "output", string(out))
		if err.Error() == "exit status 32" {
			return response, status.Error(codes.NotFound, "volume not found")
		} else {
			klog.ErrorS(err, "volume mount error", "output", out)
		}
	}

	return response, err
}

// NodeUnpublishVolume Unmount a volume from the target path
func (s *HypervCsiDriver) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	logRequest("NodeUnpublishVolume", req)

	response := &csi.NodeUnpublishVolumeResponse{}
	var err error
	out, err := exec.CommandContext(ctx, "umount", req.TargetPath).Output()
	exec.CommandContext(ctx, "/usr/bin/rmdir", req.TargetPath)
	if err != nil {
		if err.Error() == "exit status 32" {
			klog.Warningf("failed to unmount %s '%s'", req.VolumeId, string(out))
			// TODO this seemed to get stuck unless I return a normal request
			// despite the docs suggesting this error should be returned
			//return response, status.Error(codes.NotFound, "volume not found")
			return response, nil
		} else {
			klog.ErrorS(err, "volume unmount error", "output", out)
		}
	}

	return response, err
}

// NodeStageVolume Not supported capability
func (s *HypervCsiDriver) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	logRequest("NodeStageVolume", req)
	return nil, status.Error(codes.Unimplemented, "method NodeStageVolume not implemented")
}

// NodeUnstageVolume Not supported capability
func (s *HypervCsiDriver) NodeUnstageVolume(ctx context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	logRequest("NodeUnstageVolume", req)
	return nil, status.Error(codes.Unimplemented, "method NodeUnstageVolume not implemented")
}

// NodeGetVolumeStats Not supported capability
func (s *HypervCsiDriver) NodeGetVolumeStats(ctx context.Context, req *csi.NodeGetVolumeStatsRequest) (*csi.NodeGetVolumeStatsResponse, error) {
	logRequest("NodeGetVolumeStats", req)
	return nil, status.Error(codes.Unimplemented, "method NodeGetVolumeStats not implemented")
}

// NodeExpandVolume Not supported capability
func (s *HypervCsiDriver) NodeExpandVolume(ctx context.Context, req *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	logRequest("NodeExpandVolume", req)
	return nil, status.Error(codes.Unimplemented, "method NodeExpandVolume not implemented")
}
