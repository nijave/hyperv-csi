package pkg

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/go-xmlfmt/xmlfmt"
	"github.com/gofrs/uuid"
	"github.com/masterzen/winrm"
	dotnetxml "github.com/nijave/hyperv-csi/dotnet-xml"
	"github.com/sergeymakinen/go-quote/windows"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"io"
	"k8s.io/klog/v2"
	"strings"
)

type remotePowerShellRunner interface {
	RunWithContext(context.Context, string, io.Writer, io.Writer) (int, error)
}

type HypervCsiController struct {
	csi.IdentityServer
	csi.ControllerServer
	WinrmClient remotePowerShellRunner
	VolumePath  string
}

const driverName = "hyperv-csi.nijave.github.com"
const driverVersion = "1.0.0"
const defaultCapacity = 20 // GB
const volumeFilePrefix = "pv-"

const CliXmlPrefix = "#< CLIXML"

func psCommand(cmd string) string {
	cmd = winrm.Powershell(cmd)
	return strings.Replace(cmd, "powershell.exe", "powershell.exe -NoProfile", 1)
}

type PSRemoteObjects struct {
	XMLName xml.Name `xml:"Objs"`
	Objects []string `xml:"S"`
}

func parseCliXml(xmlString string) string {
	xmlString = strings.Trim(xmlString, "\r\n\t ")
	if !strings.HasPrefix(xmlString, CliXmlPrefix) {
		return xmlString
	} else {
		xmlString = strings.TrimPrefix(xmlString, CliXmlPrefix)
	}

	var psRemoteObjects PSRemoteObjects
	err := xml.Unmarshal([]byte(xmlString), &psRemoteObjects)
	if err != nil {
		klog.Warning("couldn't unmarshal Powershell objects")
		return xmlfmt.FormatXML(xmlString, "", "  ", false)
	}

	output := strings.Builder{}
	for _, str := range psRemoteObjects.Objects {
		output.WriteString(strings.Replace(dotnetxml.DecodeName(str), "\r\n", "\n", -1))
	}

	return strings.Trim(output.String(), "\r\n\t ")
}

type ExecResult struct {
	ExitCode int
	Output   string
	Error    error
}

func (s *HypervCsiController) psRun(ctx context.Context, cmd string) ExecResult {
	var bytesOut bytes.Buffer
	klog.V(8).InfoS("ps command", "command", cmd)
	exit, err := s.WinrmClient.RunWithContext(ctx, psCommand(cmd), &bytesOut, &bytesOut)
	psOutput := strings.Trim(bytesOut.String(), "\r\n\t ")
	klog.V(8).InfoS("ps raw output", "rc", exit, "output", psOutput)
	psOutput = parseCliXml(psOutput)

	return ExecResult{
		ExitCode: exit,
		Output:   psOutput,
		Error:    err,
	}
}

func (s *HypervCsiController) makeVolumePath(name string, withExtension bool) string {
	extension := ""
	if withExtension {
		extension = ".vhdx"
	}
	return windows.PSSingleQuote.Quote(s.VolumePath + "\\" + volumeFilePrefix + name + extension)
}

// IdentityServer
func (s *HypervCsiController) Probe(ctx context.Context, request *csi.ProbeRequest) (*csi.ProbeResponse, error) {
	logRequest("identity probe", request)
	return &csi.ProbeResponse{Ready: &wrapperspb.BoolValue{Value: true}}, nil
}

func (s *HypervCsiController) GetPluginInfo(ctx context.Context, request *csi.GetPluginInfoRequest) (*csi.GetPluginInfoResponse, error) {
	logRequest("identity plugin info", request)
	return &csi.GetPluginInfoResponse{
		Name:          driverName,
		VendorVersion: driverVersion,
	}, nil
}

func (s *HypervCsiController) GetPluginCapabilities(ctx context.Context, request *csi.GetPluginCapabilitiesRequest) (*csi.GetPluginCapabilitiesResponse, error) {
	logRequest("identity plugin capabilities", request)

	return &csi.GetPluginCapabilitiesResponse{
		Capabilities: []*csi.PluginCapability{
			{
				Type: &csi.PluginCapability_Service_{
					Service: &csi.PluginCapability_Service{
						Type: csi.PluginCapability_Service_CONTROLLER_SERVICE,
					},
				},
			},
		},
	}, nil
}

// ControllerServer
func (s *HypervCsiController) ListVolumes(ctx context.Context, request *csi.ListVolumesRequest) (*csi.ListVolumesResponse, error) {
	logRequest("listing volumes", request)

	listCommand := fmt.Sprintf("Get-Item %s | Select Name", s.makeVolumePath(volumeFilePrefix+"*", true))
	result := s.psRun(ctx, listCommand)

	if result.ExitCode != 0 || result.Error != nil {
		if result.Error == nil {
			result.Error = errors.New("powershell error")
		}
		klog.ErrorS(result.Error, "error listing volumes", "exitCode", result.ExitCode, "output", result.Output)
		return nil, result.Error
	}

	volumeFiles := strings.Split(result.Output, "\r")
	volumeList := make([]*csi.ListVolumesResponse_Entry, len(volumeFiles))
	for i, volumeFile := range volumeFiles {
		volumeList[i] = &csi.ListVolumesResponse_Entry{
			Volume: &csi.Volume{
				VolumeId:           strings.TrimPrefix(strings.TrimSuffix(volumeFile, ".vhdx"), volumeFilePrefix),
				CapacityBytes:      0,
				VolumeContext:      nil,
				ContentSource:      nil,
				AccessibleTopology: nil,
			},
			Status: &csi.ListVolumesResponse_VolumeStatus{
				PublishedNodeIds: nil, // OPTIONAL
				VolumeCondition:  nil, // OPTIONAL
			},
		}
	}

	return &csi.ListVolumesResponse{
		Entries:   volumeList,
		NextToken: "", // TODO pagination
	}, nil
}

func (s *HypervCsiController) CreateVolume(ctx context.Context, request *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	logRequest("creating volume", request)

	response := &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:           "",
			CapacityBytes:      0,
			VolumeContext:      nil,
			ContentSource:      nil,
			AccessibleTopology: nil,
		},
	}

	if len(request.VolumeCapabilities) > 0 {
		if request.VolumeCapabilities[0].AccessMode.GetMode() != csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER {
			capabilities := make([]string, len(request.VolumeCapabilities))
			for _, capability := range request.VolumeCapabilities {
				capabilities = append(capabilities, capability.String())
			}
			klog.InfoS("unsupported capabilities", "capabilities", strings.Join(capabilities, ","))
			return response, status.Error(codes.InvalidArgument, "")
		}
	}

	var capacity int64
	capacity = defaultCapacity * 1024 * 1024 * 1024
	if request.CapacityRange != nil {
		if request.CapacityRange.LimitBytes > 0 {
			capacity = request.CapacityRange.LimitBytes
		}
		if request.CapacityRange.RequiredBytes > 0 {
			capacity = request.CapacityRange.RequiredBytes
		}
	}

	response.Volume.CapacityBytes = capacity

	volumePath := s.makeVolumePath("temp-"+strings.Split(request.Name, "-")[1], true)
	klog.InfoS("creating volume", "path", volumePath, "size", capacity)
	// Make a temp volume based on the request ID and rename it to the VHD's GUID. Attached VHD can be located by last portion of GUID on host
	createVolumeCommand := fmt.Sprintf(`$p = %s; $id = (New-VHD -Path $p -SizeBytes %d -Dynamic).DiskIdentifier.ToLower(); Move-Item $p (Join-Path -Path (Split-Path -Parent $p) -ChildPath "%s${id}.vhdx"); echo $id`, volumePath, capacity, volumeFilePrefix)
	result := s.psRun(ctx, createVolumeCommand)

	if result.ExitCode != 0 {
		klog.Error(result.Output)
		return response, errors.New(fmt.Sprintf("command failed with exit code %d", result.ExitCode))
	} else {
		klog.Info(result.Output)
	}

	hopefullyUuid := strings.Trim(result.Output, "\r\n")
	if _, err := uuid.FromString(hopefullyUuid); err != nil {
		psError := errors.New("unexpected New-VHD output. Expected parseable uuid")
		klog.ErrorS(psError, "message", hopefullyUuid)
		return nil, psError
	}

	response.Volume.VolumeId = hopefullyUuid
	return response, result.Error
}

func (s *HypervCsiController) DeleteVolume(ctx context.Context, request *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	logRequest("deleting volume", request)
	response := &csi.DeleteVolumeResponse{}

	deleteCommand := psCommand(fmt.Sprintf("Remove-Item -Force (%s+\"*\")", s.makeVolumePath(request.VolumeId, false)))
	result := s.psRun(ctx, deleteCommand)

	if result.ExitCode != 0 {
		err := errors.New("powershell error")
		klog.ErrorS(err, "exitCode", result.ExitCode, "output", result.Output)
		return response, err
	}

	if strings.Contains(result.Output, "failed to delete attached volume") {
		klog.Errorf("volume %s not found in volume list", request.VolumeId)
		return response, errors.New("powershell error")
	}

	return response, result.Error
}

func (s *HypervCsiController) ValidateVolumeCapabilities(ctx context.Context, request *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	response := &csi.ValidateVolumeCapabilitiesResponse{
		Confirmed: nil,
		Message:   "",
	}

	responseCapabilities := make([]*csi.VolumeCapability, 0)
	for _, capability := range request.VolumeCapabilities {
		// Not sure if this is the right way to switch on capability...
		switch capability.GetAccessMode().GetMode() {
		case csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER:
			responseCapabilities = append(responseCapabilities, &csi.VolumeCapability{
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
				},
				AccessType: &csi.VolumeCapability_Mount{
					// All fields are optional
					Mount: &csi.VolumeCapability_MountVolume{},
				},
			})
		default:
		}
	}

	response.Confirmed = &csi.ValidateVolumeCapabilitiesResponse_Confirmed{
		VolumeCapabilities: responseCapabilities,
		VolumeContext:      request.VolumeContext,
		Parameters:         nil,
	}

	return response, nil
}

func (s *HypervCsiController) ControllerGetCapabilities(ctx context.Context, request *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
	response := &csi.ControllerGetCapabilitiesResponse{
		Capabilities: []*csi.ControllerServiceCapability{
			{
				Type: &csi.ControllerServiceCapability_Rpc{
					Rpc: &csi.ControllerServiceCapability_RPC{
						Type: csi.ControllerServiceCapability_RPC_LIST_VOLUMES,
					},
				},
			},
			{
				Type: &csi.ControllerServiceCapability_Rpc{
					Rpc: &csi.ControllerServiceCapability_RPC{
						Type: csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
					},
				},
			},
			{
				Type: &csi.ControllerServiceCapability_Rpc{
					Rpc: &csi.ControllerServiceCapability_RPC{
						Type: csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME,
					},
				},
			},
		},
	}
	return response, nil
}

type vhdParentChild struct {
	Vhd    string `json:"Path"`
	Parent string `json:"ParentPath"`
}

func (s *HypervCsiController) ControllerPublishVolume(ctx context.Context, request *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	// TODO v1 attach VHD to VM (last one if there's snapshots...)
	cmd := fmt.Sprintf("ConvertTo-Json @(Get-VHD (%s+\"*\") | Select ParentPath, Path)", s.makeVolumePath(request.VolumeId, false))
	result := s.psRun(ctx, cmd)

	if result.ExitCode != 0 {
		klog.InfoS("powershell error", "output", result)
		return nil, errors.New("powershell error")
	}

	var parentChildList []vhdParentChild
	err := json.Unmarshal([]byte(result.Output), &parentChildList)
	if err != nil {
		klog.Warning("couldn't unmarshal parent-child vhd list json")
		klog.InfoS("json unmarshal error", "output", result)
		return nil, err
	}
	parentChild := map[string]string{}
	for _, vhd := range parentChildList {
		parentChild[vhd.Parent] = vhd.Vhd
	}
	lastParent := ""
	for {
		nextParent, ok := parentChild[lastParent]
		if !ok {
			break
		}
		lastParent = nextParent
	}
	klog.InfoS("attaching vhd", "vhd", lastParent, "node", request.NodeId)
	// Add-VMHardDiskDrive -VMName vmubt2204kube04 -ControllerType SCSI -ControllerNumber 0 -Path "v:\\hyper-v\\virtual hard disks\\pvc-583055da-f7b4-474f-9bea-59d346c21509.vhdx"
	cmd = fmt.Sprintf("Add-VMHardDiskDrive -VMName %s -ControllerType SCSI -ControllerNumber 0 -Path '%s'", request.NodeId, lastParent)
	result = s.psRun(ctx, cmd)

	// Idempotence
	if strings.Contains(result.Output, "The disk is already connect to the virtual machine") {
		result.ExitCode = 0
		result.Error = nil
	}

	if result.ExitCode != 0 && result.Error == nil {
		result.Error = errors.New("powershell error")
	}
	if result.Error != nil {
		return nil, result.Error
	}

	return &csi.ControllerPublishVolumeResponse{
		PublishContext: map[string]string{},
	}, nil
}

func (s *HypervCsiController) ControllerUnpublishVolume(ctx context.Context, request *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	// Get-VMHardDiskDrive -VMName vmubt2204kube04 | Where-Object {$_.Path -like "*pvc-583055da-f7b4-474f-9bea-59d346c21509*"} | Remove-VMHardDiskDrive
	cmd := fmt.Sprintf("Get-VMHardDiskDrive -VMName %s | Where-Object {$_.Path -like \"*%s*\"} | Remove-VMHardDiskDrive", request.NodeId, request.VolumeId)
	result := s.psRun(ctx, cmd)
	if result.ExitCode != 0 && result.Error == nil {
		result.Error = errors.New("powershell error")
	}
	if result.Error != nil {
		return nil, result.Error
	}

	return &csi.ControllerUnpublishVolumeResponse{}, nil
}

func (s *HypervCsiController) GetCapacity(ctx context.Context, request *csi.GetCapacityRequest) (*csi.GetCapacityResponse, error) {
	// TODO v2
	return nil, status.Error(codes.Unimplemented, "")
}

func (s *HypervCsiController) ControllerExpandVolume(ctx context.Context, request *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	// TODO v2
	return nil, status.Error(codes.Unimplemented, "")
}

func (s *HypervCsiController) ControllerGetVolume(ctx context.Context, request *csi.ControllerGetVolumeRequest) (*csi.ControllerGetVolumeResponse, error) {
	// TODO v2
	return nil, status.Error(codes.Unimplemented, "")
}

func (s *HypervCsiController) ListSnapshots(ctx context.Context, request *csi.ListSnapshotsRequest) (*csi.ListSnapshotsResponse, error) {
	// TODO v3
	return nil, status.Error(codes.Unimplemented, "")
}

func (s *HypervCsiController) CreateSnapshot(ctx context.Context, request *csi.CreateSnapshotRequest) (*csi.CreateSnapshotResponse, error) {
	// TODO v3
	return nil, status.Error(codes.Unimplemented, "")
}

func (s *HypervCsiController) DeleteSnapshot(ctx context.Context, request *csi.DeleteSnapshotRequest) (*csi.DeleteSnapshotResponse, error) {
	// TODO v3
	return nil, status.Error(codes.Unimplemented, "")
}
