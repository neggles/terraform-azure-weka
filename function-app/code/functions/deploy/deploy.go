package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"weka-deployment/common"
	"weka-deployment/functions/azure_functions_def"

	"github.com/weka/go-cloud-lib/deploy"
	"github.com/weka/go-cloud-lib/functions_def"
	"github.com/weka/go-cloud-lib/join"
	"github.com/weka/go-cloud-lib/logging"
	"github.com/weka/go-cloud-lib/protocol"

	"github.com/lithammer/dedent"
)

type AzureDeploymentParams struct {
	SubscriptionId        string
	ResourceGroupName     string
	StateParams           common.BlobObjParams
	Prefix                string
	ClusterName           string
	InstallUrl            string
	KeyVaultUri           string
	ProxyUrl              string
	VmName                string
	ComputeMemory         string
	ComputeContainerNum   int
	FrontendContainerNum  int
	DriveContainerNum     int
	DiskSize              int
	InstallDpdk           bool
	NicsNum               string
	FunctionAppName       string
	Gateways              []string
	NFSInterfaceGroupName string
	NFSClientGroupName    string
	NFSProtocolGWsNum     int
	NFSGWFeCoresNum       int
	NFSStateParams        common.BlobObjParams
	NFSSecondaryIpsNum    int
	NFSVmssName           string
	NFSDiskSize           int
	BackendLbIp           string
}

func GetDeviceName(diskSize int) string {
	// wekaiosw_device=/dev/"$(lsblk | grep ${disk_size}G | awk '{print $1}')"
	template := "/dev/\"$(lsblk | grep %dG | awk '{print $1}')\""
	return fmt.Sprintf(template, diskSize)
}

func GetAzurePrimaryIpCmd() string {
	return "curl -s -H Metadata:true --noproxy '*' http://169.254.169.254/metadata/instance/network/interface/0/ipv4/ipAddress/0?api-version=2023-07-01 | jq -r '.privateIpAddress'"
}

func getWekaIoToken(ctx context.Context, keyVaultUri string) (token string, err error) {
	token, err = common.GetKeyVaultValue(ctx, keyVaultUri, "get-weka-io-token")
	return
}

func getFunctionKey(ctx context.Context, keyVaultUri string) (functionAppKey string, err error) {
	functionAppKey, err = common.GetKeyVaultValue(ctx, keyVaultUri, "function-app-default-key")
	return
}

func GetNfsDeployScript(ctx context.Context, funcDef functions_def.FunctionDef, p AzureDeploymentParams) (bashScript string, err error) {
	logger := logging.LoggerFromCtx(ctx)
	logger.Info().Msg("Getting NFS deploy script")

	state, err := common.ReadState(ctx, p.NFSStateParams)
	if err != nil {
		logger.Error().Err(err).Send()
		return
	}
	instanceParams := protocol.BackendCoreCount{
		Compute:       p.ComputeContainerNum,
		Frontend:      p.FrontendContainerNum,
		Drive:         p.DriveContainerNum,
		ComputeMemory: p.ComputeMemory,
	}

	wekaPassword, err := common.GetWekaClusterPassword(ctx, p.KeyVaultUri)
	if err != nil {
		logger.Error().Err(err).Send()
		return
	}

	var token string
	token, err = getWekaIoToken(ctx, p.KeyVaultUri)
	if err != nil {
		return
	}

	deploymentParams := deploy.DeploymentParams{
		VMName:                       p.VmName,
		InstanceParams:               instanceParams,
		WekaInstallUrl:               p.InstallUrl,
		WekaToken:                    token,
		NicsNum:                      p.NicsNum,
		InstallDpdk:                  p.InstallDpdk,
		ProxyUrl:                     p.ProxyUrl,
		Gateways:                     p.Gateways,
		Protocol:                     protocol.NFS,
		WekaUsername:                 common.WekaAdminUsername,
		WekaPassword:                 wekaPassword,
		NFSInterfaceGroupName:        p.NFSInterfaceGroupName,
		NFSClientGroupName:           p.NFSClientGroupName,
		NFSSecondaryIpsNum:           p.NFSSecondaryIpsNum,
		NFSProtocolGatewayFeCoresNum: p.NFSGWFeCoresNum,
		LoadBalancerIP:               p.BackendLbIp,
		GetPrimaryIpCmd:              GetAzurePrimaryIpCmd(),
	}

	if !state.Clusterized {
		deployScriptGenerator := deploy.DeployScriptGenerator{
			FuncDef:       funcDef,
			Params:        deploymentParams,
			DeviceNameCmd: GetDeviceName(p.NFSDiskSize),
		}
		bashScript = deployScriptGenerator.GetDeployScript()
	} else {
		joinScriptGenerator := join.JoinNFSScriptGenerator{
			DeviceNameCmd:      GetDeviceName(p.NFSDiskSize),
			DeploymentParams:   deploymentParams,
			InterfaceGroupName: p.NFSInterfaceGroupName,
			FuncDef:            funcDef,
		}
		bashScript = joinScriptGenerator.GetJoinNFSHostScript()
	}

	return
}

func GetDeployScript(ctx context.Context, funcDef functions_def.FunctionDef, p AzureDeploymentParams) (bashScript string, err error) {
	logger := logging.LoggerFromCtx(ctx)

	state, err := common.ReadState(ctx, p.StateParams)
	if err != nil {
		return
	}

	instanceParams := protocol.BackendCoreCount{
		Compute:       p.ComputeContainerNum,
		Frontend:      p.FrontendContainerNum,
		Drive:         p.DriveContainerNum,
		ComputeMemory: p.ComputeMemory,
	}
	if err != nil {
		logger.Error().Err(err).Send()
		return "", err
	}

	if !state.Clusterized {
		var token string
		token, err = getWekaIoToken(ctx, p.KeyVaultUri)
		if err != nil {
			return
		}

		deploymentParams := deploy.DeploymentParams{
			VMName:         p.VmName,
			InstanceParams: instanceParams,
			WekaInstallUrl: p.InstallUrl,
			WekaToken:      token,
			InstallDpdk:    p.InstallDpdk,
			NicsNum:        p.NicsNum,
			Gateways:       p.Gateways,
			ProxyUrl:       p.ProxyUrl,
		}
		deployScriptGenerator := deploy.DeployScriptGenerator{
			FuncDef:       funcDef,
			Params:        deploymentParams,
			DeviceNameCmd: GetDeviceName(p.DiskSize),
		}
		bashScript = deployScriptGenerator.GetDeployScript()
	} else {
		wekaPassword, err := common.GetWekaClusterPassword(ctx, p.KeyVaultUri)
		if err != nil {
			logger.Error().Err(err).Send()
			return "", err
		}

		vmScaleSetName := common.GetVmScaleSetName(p.Prefix, p.ClusterName)
		vmssParams := &common.ScaleSetParams{
			SubscriptionId:    p.SubscriptionId,
			ResourceGroupName: p.ResourceGroupName,
			ScaleSetName:      vmScaleSetName,
			Flexible:          false,
		}
		vmsPrivateIps, err := common.GetVmsPrivateIps(ctx, vmssParams)
		if err != nil {
			logger.Error().Err(err).Send()
			return "", err
		}

		vmNameParts := strings.Split(p.VmName, ":")
		vmName := vmNameParts[0]

		var ips []string
		for ipVmName, ip := range vmsPrivateIps {
			// exclude ip of the machine itself
			if ipVmName != vmName {
				ips = append(ips, ip)
			}
		}
		if len(ips) == 0 {
			err = fmt.Errorf("no instances found for scale set %s, can't join", vmScaleSetName)
			logger.Error().Err(err).Send()
			return "", err
		}

		joinParams := join.JoinParams{
			WekaUsername:   common.WekaAdminUsername,
			WekaPassword:   wekaPassword,
			IPs:            ips,
			InstallDpdk:    p.InstallDpdk,
			InstanceParams: instanceParams,
			Gateways:       p.Gateways,
			ProxyUrl:       p.ProxyUrl,
		}

		scriptBase := `
		#!/bin/bash
		set -ex
		`

		joinScriptGenerator := join.JoinScriptGenerator{
			GetInstanceNameCmd: common.GetAzureInstanceNameCmd(),
			FindDrivesScript:   dedent.Dedent(common.FindDrivesScript),
			ScriptBase:         dedent.Dedent(scriptBase),
			Params:             joinParams,
			FuncDef:            funcDef,
		}
		bashScript = joinScriptGenerator.GetJoinScript(ctx)
	}
	bashScript = dedent.Dedent(bashScript)
	return
}

func getGateway(subnet string) string {
	ip, ipNet, _ := net.ParseCIDR(subnet)
	ip = ip.Mask(ipNet.Mask)
	for i := len(ip) - 1; i >= 0; i-- {
		ip[i]++
		if ip[i] > 0 {
			break
		}
	}
	return ip.String()
}

func getGateways(subnet string, nicsNum int) (gateways []string) {
	gateway := getGateway(subnet)
	gateways = make([]string, nicsNum)
	for i := range gateways {
		gateways[i] = gateway
	}
	return
}

func Handler(w http.ResponseWriter, r *http.Request) {
	stateContainerName := os.Getenv("STATE_CONTAINER_NAME")
	stateStorageName := os.Getenv("STATE_STORAGE_NAME")
	stateBlobName := os.Getenv("STATE_BLOB_NAME")
	clusterName := os.Getenv("CLUSTER_NAME")
	subscriptionId := os.Getenv("SUBSCRIPTION_ID")
	resourceGroupName := os.Getenv("RESOURCE_GROUP_NAME")
	prefix := os.Getenv("PREFIX")
	keyVaultUri := os.Getenv("KEY_VAULT_URI")
	computeMemory := os.Getenv("COMPUTE_MEMORY")
	computeContainerNum, _ := strconv.Atoi(os.Getenv("COMPUTE_CONTAINER_CORES_NUM"))
	frontendContainerNum, _ := strconv.Atoi(os.Getenv("FRONTEND_CONTAINER_CORES_NUM"))
	driveContainerNum, _ := strconv.Atoi(os.Getenv("DRIVE_CONTAINER_CORES_NUM"))
	installDpdk, _ := strconv.ParseBool(os.Getenv("INSTALL_DPDK"))
	nicsNum := os.Getenv("NICS_NUM")
	nicsNumInt, _ := strconv.Atoi(nicsNum)
	subnet := os.Getenv("SUBNET")
	functionAppName := os.Getenv("FUNCTION_APP_NAME")
	diskSize, _ := strconv.Atoi(os.Getenv("DISK_SIZE"))
	// nfs params
	nfsInterfaceGroupName := os.Getenv("NFS_INTERFACE_GROUP_NAME")
	nfsClientGroupName := os.Getenv("NFS_CLIENT_GROUP_NAME")
	nfsProtocolgwsNum, _ := strconv.Atoi(os.Getenv("NFS_PROTOCOL_GATEWAYS_NUM"))
	nfsStateContainerName := os.Getenv("NFS_STATE_CONTAINER_NAME")
	nfsStateBlobName := os.Getenv("NFS_STATE_BLOB_NAME")
	nfsSecondaryIpsNum, _ := strconv.Atoi(os.Getenv("NFS_SECONDARY_IPS_NUM"))
	nfsProtocolGatewayFeCoresNum, _ := strconv.Atoi(os.Getenv("NFS_PROTOCOL_GATEWAY_FE_CORES_NUM"))
	nfsVmssName := os.Getenv("NFS_VMSS_NAME")
	nfsDiskSize, _ := strconv.Atoi(os.Getenv("NFS_DISK_SIZE"))
	backendLbIp := os.Getenv("BACKEND_LB_IP")

	installUrl := os.Getenv("INSTALL_URL")
	proxyUrl := os.Getenv("PROXY_URL")

	var invokeRequest common.InvokeRequest

	ctx := r.Context()
	logger := logging.LoggerFromCtx(ctx)

	d := json.NewDecoder(r.Body)
	err := d.Decode(&invokeRequest)
	if err != nil {
		err = fmt.Errorf("cannot decode the request: %v", err)
		logger.Error().Err(err).Send()
		common.WriteErrorResponse(w, err)
		return
	}

	var reqData map[string]interface{}
	err = json.Unmarshal(invokeRequest.Data["req"], &reqData)
	if err != nil {
		err = fmt.Errorf("cannot unmarshal the request data: %v", err)
		logger.Error().Err(err).Send()
		common.WriteErrorResponse(w, err)
		return
	}

	var vm protocol.Vm
	if err := json.Unmarshal([]byte(reqData["Body"].(string)), &vm); err != nil {
		err = fmt.Errorf("cannot unmarshal the request body: %v", err)
		logger.Error().Err(err).Send()
		common.WriteErrorResponse(w, err)
		return
	}

	params := AzureDeploymentParams{
		SubscriptionId:        subscriptionId,
		ResourceGroupName:     resourceGroupName,
		StateParams:           common.BlobObjParams{StorageName: stateStorageName, ContainerName: stateContainerName, BlobName: stateBlobName},
		Prefix:                prefix,
		ClusterName:           clusterName,
		InstallUrl:            installUrl,
		KeyVaultUri:           keyVaultUri,
		ProxyUrl:              proxyUrl,
		VmName:                vm.Name,
		ComputeMemory:         computeMemory,
		ComputeContainerNum:   computeContainerNum,
		FrontendContainerNum:  frontendContainerNum,
		DriveContainerNum:     driveContainerNum,
		DiskSize:              diskSize,
		InstallDpdk:           installDpdk,
		NicsNum:               nicsNum,
		FunctionAppName:       functionAppName,
		Gateways:              getGateways(subnet, nicsNumInt),
		NFSInterfaceGroupName: nfsInterfaceGroupName,
		NFSClientGroupName:    nfsClientGroupName,
		NFSProtocolGWsNum:     nfsProtocolgwsNum,
		NFSStateParams:        common.BlobObjParams{StorageName: stateStorageName, ContainerName: nfsStateContainerName, BlobName: nfsStateBlobName},
		NFSSecondaryIpsNum:    nfsSecondaryIpsNum,
		NFSGWFeCoresNum:       nfsProtocolGatewayFeCoresNum,
		NFSVmssName:           nfsVmssName,
		NFSDiskSize:           nfsDiskSize,
		BackendLbIp:           backendLbIp,
	}

	// create Function Definer
	functionKey, err := getFunctionKey(ctx, params.KeyVaultUri)
	if err != nil {
		return
	}
	baseFunctionUrl := fmt.Sprintf("https://%s.azurewebsites.net/api/", params.FunctionAppName)
	funcDef := azure_functions_def.NewFuncDef(baseFunctionUrl, functionKey)

	var bashScript string
	if vm.Protocol == protocol.NFS {
		bashScript, err = GetNfsDeployScript(ctx, funcDef, params)
	} else {
		bashScript, err = GetDeployScript(ctx, funcDef, params)
	}

	if err != nil {
		common.WriteErrorResponse(w, err)
		return
	}
	common.WriteSuccessResponse(w, bashScript)
}
