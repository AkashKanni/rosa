package profilehandler

import (
	"fmt"
	"strings"
	"time"

	"github.com/openshift/rosa/pkg/ocm"
	"github.com/openshift/rosa/tests/ci/config"
	"github.com/openshift/rosa/tests/utils/common"
	con "github.com/openshift/rosa/tests/utils/common/constants"
	ClusterConfigure "github.com/openshift/rosa/tests/utils/config"
	"github.com/openshift/rosa/tests/utils/exec/rosacli"
	"github.com/openshift/rosa/tests/utils/log"
)

var client rosacli.Client

func init() {
	client = *rosacli.NewClient()
}

func GetYAMLProfilesDir() string {
	return config.Test.YAMLProfilesDir
}
func LoadProfileYamlFile(profileName string) *Profile {
	p := GetProfile(profileName, GetYAMLProfilesDir())
	log.Logger.Infof("Loaded cluster profile configuration from origional profile %s : %v", profileName, *p)
	log.Logger.Infof("Loaded cluster profile configuration from origional cluster %s : %v", profileName, *p.ClusterConfig)
	log.Logger.Infof("Loaded cluster profile configuration from origional account-roles %s : %v", profileName, *p.AccountRoleConfig)
	if p.NamePrefix == "" {
		p.NamePrefix = con.DefaultNamePrefix
	}
	return p
}

func LoadProfileYamlFileByENV() *Profile {
	if config.Test.TestProfile == "" {
		panic(fmt.Errorf("ENV Variable TEST_PROFILE is empty, please make sure you set the env value"))
	}
	profile := LoadProfileYamlFile(config.Test.TestProfile)

	// Supporting global env setting to overrite profile settings
	if config.Test.GlobalENV.ChannelGroup != "" {
		log.Logger.Infof("Got global env settings for CHANNEL_GROUP, overwritten the profile setting with value %s",
			config.Test.GlobalENV.ChannelGroup)
		profile.ChannelGroup = config.Test.GlobalENV.ChannelGroup
	}
	if config.Test.GlobalENV.Version != "" {
		log.Logger.Infof("Got global env settings for VERSION, overwritten the profile setting with value %s",
			config.Test.GlobalENV.Version)
		profile.Version = config.Test.GlobalENV.Version
	}
	if config.Test.GlobalENV.Region != "" {
		log.Logger.Infof("Got global env settings for REGION, overwritten the profile setting with value %s",
			config.Test.GlobalENV.Region)
		profile.Region = config.Test.GlobalENV.Region
	}
	if config.Test.GlobalENV.ProvisionShard != "" {
		log.Logger.Infof("Got global env settings for PROVISION_SHARD, overwritten the profile setting with value %s",
			config.Test.GlobalENV.ProvisionShard)
		profile.ClusterConfig.ProvisionShard = config.Test.GlobalENV.ProvisionShard
	}
	if config.Test.GlobalENV.NamePrefix != "" {
		log.Logger.Infof("Got global env settings for NAME_PREFIX, overwritten the profile setting with value %s",
			config.Test.GlobalENV.NamePrefix)
		profile.NamePrefix = config.Test.GlobalENV.NamePrefix
	}

	return profile
}

// GenerateAccountRoleCreationFlag will generate account role creation flags
func GenerateAccountRoleCreationFlag(client *rosacli.Client,
	namePrefix string,
	hcp bool,
	openshiftVersion string,
	channelGroup string,
	path string,
	permissionsBoundary string) []string {
	flags := []string{
		"--prefix", namePrefix,
		"--mode", "auto",
		"-y",
	}
	if openshiftVersion != "" {
		majorVersion := common.SplitMajorVersion(openshiftVersion)
		flags = append(flags, "--version", majorVersion)
	}
	if channelGroup != "" {
		flags = append(flags, "--channel-group", channelGroup)
	}
	if hcp {
		flags = append(flags, "--hosted-cp")
	} else {
		flags = append(flags, "--classic")
	}
	if path != "" {
		flags = append(flags, "--path", path)
	}
	if permissionsBoundary != "" {
		flags = append(flags, "--permissions-boundary", permissionsBoundary)
	}
	return flags

}

// GenerateClusterCreateFlags will generate cluster creation flags
func GenerateClusterCreateFlags(profile *Profile, client *rosacli.Client) ([]string, error) {
	if profile.ClusterConfig.NameLegnth == 0 {
		profile.ClusterConfig.NameLegnth = con.DefaultNameLength //Set to a default value when it is not set
	}
	clusterName := PreparePrefix(profile.NamePrefix, profile.ClusterConfig.NameLegnth)
	profile.ClusterConfig.Name = clusterName
	var clusterConfiguration = new(ClusterConfigure.ClusterConfig)
	var userData = new(UserData)
	defer func() {

		// Record userdata
		_, err := common.CreateFileWithContent(config.Test.UserDataFile, userData)
		if err != nil {
			log.Logger.Errorf("Cannot record user data: %s", err.Error())
			panic(fmt.Errorf("cannot record user data: %s", err.Error()))
		}

		// Record cluster configuration

		_, err = common.CreateFileWithContent(config.Test.ClusterConfigFile, clusterConfiguration)
		if err != nil {
			log.Logger.Errorf("Cannot record cluster configuration: %s", err.Error())
			panic(fmt.Errorf("cannot record cluster configuration: %s", err.Error()))
		}
	}()
	flags := []string{}
	clusterConfiguration.Name = clusterName

	if profile.Version != "" {
		version, err := PrepareVersion(client, profile.Version, profile.ChannelGroup, profile.ClusterConfig.HCP)
		if err != nil {
			return flags, err
		}
		profile.Version = version.Version
		flags = append(flags, "--version", version.Version)

		clusterConfiguration.Version = &ClusterConfigure.Version{
			ChannelGroup: profile.ChannelGroup,
			RawID:        version.Version,
		}
	}
	if profile.ChannelGroup != "" {
		flags = append(flags, "--channel-group", profile.ChannelGroup)
		if clusterConfiguration.Version == nil {
			clusterConfiguration.Version = &ClusterConfigure.Version{}
		}
		clusterConfiguration.Version.ChannelGroup = profile.ChannelGroup
	}
	if profile.Region != "" {
		flags = append(flags, "--region", profile.Region)
		clusterConfiguration.Region = profile.Region
	}
	if profile.ClusterConfig.DomainPrefixEnabled {
		flags = append(flags,
			"--domain-prefix", common.TrimNameByLength(clusterName, ocm.MaxClusterDomainPrefixLength),
		)

	}
	if profile.ClusterConfig.STS {
		var accRoles *rosacli.AccountRolesUnit
		var oidcConfigID string
		accountRolePrefix := common.TrimNameByLength(clusterName, con.MaxRolePrefixLength)
		log.Logger.Infof("Got sts set to true. Going to prepare Account roles with prefix %s", accountRolePrefix)
		accRoles, err := PrepareAccountRoles(
			client, accountRolePrefix,
			profile.ClusterConfig.HCP,
			profile.Version,
			profile.ChannelGroup,
			profile.AccountRoleConfig.Path,
			profile.AccountRoleConfig.PermissionBoundary,
		)
		if err != nil {
			log.Logger.Errorf("Got error happens when prepare account roles: %s", err.Error())
			return flags, err
		}
		userData.AccountRolesPrefix = accountRolePrefix
		flags = append(flags,
			"--role-arn", accRoles.InstallerRole,
			"--support-role-arn", accRoles.SupportRole,
			"--worker-iam-role", accRoles.WorkerRole,
		)

		clusterConfiguration.Sts = true
		clusterConfiguration.Aws = &ClusterConfigure.AWS{
			Sts: ClusterConfigure.Sts{
				RoleArn:        accRoles.InstallerRole,
				SupportRoleArn: accRoles.SupportRole,
				WorkerRoleArn:  accRoles.WorkerRole,
			},
		}
		if !profile.ClusterConfig.HCP {
			flags = append(flags,
				"--controlplane-iam-role", accRoles.ControlPlaneRole,
			)
			clusterConfiguration.Aws.Sts.ControlPlaneRoleArn = accRoles.ControlPlaneRole

		}
		operatorRolePrefix := accountRolePrefix
		if profile.ClusterConfig.OIDCConfig != "" {
			oidcConfigPrefix := common.TrimNameByLength(clusterName, con.MaxOIDCConfigPrefixLength)
			log.Logger.Infof("Got  oidc config setting, going to prepare the %s oidc with prefix %s",
				profile.ClusterConfig.OIDCConfig, oidcConfigPrefix)
			oidcConfigID, err = PrepareOIDCConfig(client, profile.ClusterConfig.OIDCConfig,
				profile.Region, accRoles.InstallerRole, oidcConfigPrefix)
			if err != nil {
				return flags, err
			}
			err = PrepareOIDCProvider(client, oidcConfigID)
			if err != nil {
				return flags, err
			}
			err = PrepareOperatorRolesByOIDCConfig(client, operatorRolePrefix,
				oidcConfigID, accRoles.InstallerRole, "", profile.ClusterConfig.HCP, profile.ChannelGroup)
			if err != nil {
				return flags, err
			}
			flags = append(flags, "--oidc-config-id", oidcConfigID)
			clusterConfiguration.Aws.Sts.OidcConfigID = oidcConfigID
			userData.OIDCConfigID = oidcConfigID
		}

		flags = append(flags, "--operator-roles-prefix", operatorRolePrefix)
		clusterConfiguration.Aws.Sts.OperatorRolesPrefix = operatorRolePrefix
		userData.OperatorRolesPrefix = operatorRolePrefix

		if profile.ClusterConfig.AuditLogForward {
			auditLogRoleName := accountRolePrefix
			auditRoleArn, err := PrepareAuditlogRoleArnByOIDCConfig(client, auditLogRoleName, oidcConfigID, profile.Region)
			clusterConfiguration.AuditLogArn = auditRoleArn
			userData.AuditLogArn = auditRoleArn
			if err != nil {
				return flags, err
			}
			flags = append(flags,
				"--audit-log-arn", auditRoleArn)
		}
	}

	if profile.ClusterConfig.AdminEnabled {
		// Comment below part due to OCM-7112
		log.Logger.Infof("Day1 admin is enabled. Going to generate the admin user and password and record in %s",
			config.Test.ClusterAdminFile)
		_, password := PrepareAdminUser() // Unuse cluster-admin right now
		userName := "cluster-admin"

		flags = append(flags,
			"--create-admin-user",
			"--cluster-admin-password", password,
			// "--cluster-admin-user", userName,
		)
		common.CreateFileWithContent(config.Test.ClusterAdminFile, fmt.Sprintf("%s:%s", userName, password))
	}

	if profile.ClusterConfig.Autoscale {
		minReplicas := "3"
		maxRelicas := "6"
		flags = append(flags,
			"--enable-autoscaling",
			"--min-replicas", minReplicas,
			"--max-replicas", maxRelicas,
		)
		clusterConfiguration.Autoscaling = &ClusterConfigure.Autoscaling{
			Enabled: true,
		}
		clusterConfiguration.Nodes = &ClusterConfigure.Nodes{
			MinReplicas: minReplicas,
			MaxReplicas: maxRelicas,
		}
	}
	if profile.ClusterConfig.WorkerPoolReplicas != 0 {
		flags = append(flags, "--replicas", fmt.Sprintf("%v", profile.ClusterConfig.WorkerPoolReplicas))
		clusterConfiguration.Nodes = &ClusterConfigure.Nodes{
			Replicas: fmt.Sprintf("%v", profile.ClusterConfig.WorkerPoolReplicas),
		}
	}

	if profile.ClusterConfig.IngressCustomized {
		clusterConfiguration.IngressConfig = &ClusterConfigure.IngressConfig{
			DefaultIngressRouteSelector:            "app1=test1,app2=test2",
			DefaultIngressExcludedNamespaces:       "test-ns1,test-ns2",
			DefaultIngressWildcardPolicy:           "WildcardsDisallowed",
			DefaultIngressNamespaceOwnershipPolicy: "Strict",
		}
		flags = append(flags,
			"--default-ingress-route-selector", clusterConfiguration.IngressConfig.DefaultIngressRouteSelector,
			"--default-ingress-excluded-namespaces", clusterConfiguration.IngressConfig.DefaultIngressExcludedNamespaces,
			"--default-ingress-wildcard-policy", clusterConfiguration.IngressConfig.DefaultIngressWildcardPolicy,
			"--default-ingress-namespace-ownership-policy", clusterConfiguration.IngressConfig.DefaultIngressNamespaceOwnershipPolicy,
		)
	}
	if profile.ClusterConfig.AutoscalerEnabled {
		autoscaler := &ClusterConfigure.Autoscaler{
			AutoscalerBalanceSimilarNodeGroups:    true,
			AutoscalerSkipNodesWithLocalStorage:   true,
			AutoscalerLogVerbosity:                "4",
			AutoscalerMaxPodGracePeriod:           "0",
			AutoscalerPodPriorityThreshold:        "0",
			AutoscalerIgnoreDaemonsetsUtilization: true,
			AutoscalerMaxNodeProvisionTime:        "10m",
			AutoscalerBalancingIgnoredLabels:      "aaa",
			AutoscalerMaxNodesTotal:               "1000",
			AutoscalerMinCores:                    "0",
			AutoscalerMaxCores:                    "100",
			AutoscalerMinMemory:                   "0",
			AutoscalerMaxMemory:                   "4096",
			// AutoscalerGpuLimit:                      "1",
			AutoscalerScaleDownEnabled:              true,
			AutoscalerScaleDownUtilizationThreshold: "0.5",
			AutoscalerScaleDownDelayAfterAdd:        "10s",
			AutoscalerScaleDownDelayAfterDelete:     "10s",
			AutoscalerScaleDownDelayAfterFailure:    "10s",
			// AutoscalerScaleDownUnneededTime:         "3m",
		}
		flags = append(flags,
			"--autoscaler-balance-similar-node-groups",
			"--autoscaler-skip-nodes-with-local-storage",
			"--autoscaler-log-verbosity", autoscaler.AutoscalerLogVerbosity,
			"--autoscaler-max-pod-grace-period", autoscaler.AutoscalerMaxPodGracePeriod,
			"--autoscaler-pod-priority-threshold", autoscaler.AutoscalerPodPriorityThreshold,
			"--autoscaler-ignore-daemonsets-utilization",
			"--autoscaler-max-node-provision-time", autoscaler.AutoscalerMaxNodeProvisionTime,
			"--autoscaler-balancing-ignored-labels", autoscaler.AutoscalerBalancingIgnoredLabels,
			"--autoscaler-max-nodes-total", autoscaler.AutoscalerMaxNodesTotal,
			"--autoscaler-min-cores", autoscaler.AutoscalerMinCores,
			"--autoscaler-max-cores", autoscaler.AutoscalerMaxCores,
			"--autoscaler-min-memory", autoscaler.AutoscalerMinMemory,
			"--autoscaler-max-memory", autoscaler.AutoscalerMaxMemory,
			// "--autoscaler-gpu-limit", autoscaler.AutoscalerGpuLimit,
			"--autoscaler-scale-down-enabled",
			// "--autoscaler-scale-down-unneeded-time", autoscaler.AutoscalerScaleDownUnneededTime,
			"--autoscaler-scale-down-utilization-threshold", autoscaler.AutoscalerScaleDownUtilizationThreshold,
			"--autoscaler-scale-down-delay-after-add", autoscaler.AutoscalerScaleDownDelayAfterAdd,
			"--autoscaler-scale-down-delay-after-delete", autoscaler.AutoscalerScaleDownDelayAfterDelete,
			"--autoscaler-scale-down-delay-after-failure", autoscaler.AutoscalerScaleDownDelayAfterFailure,
		)

		clusterConfiguration.Autoscaler = autoscaler
	}
	if profile.ClusterConfig.NetworkingSet {
		networking := &ClusterConfigure.Networking{
			MachineCIDR: "10.0.0.0/16",
			PodCIDR:     "192.168.0.0/18",
			ServiceCIDR: "172.31.0.0/24",
			HostPrefix:  "25",
		}
		flags = append(flags,
			"--machine-cidr", networking.MachineCIDR, // Placeholder, it should be vpc CIDR
			"--service-cidr", networking.ServiceCIDR,
			"--pod-cidr", networking.PodCIDR,
			"--host-prefix", networking.HostPrefix,
		)
		clusterConfiguration.Networking = networking
	}
	if profile.ClusterConfig.BYOVPC {
		vpcPrefix := common.TrimNameByLength(clusterName, 20)
		log.Logger.Info("Got BYOVPC set to true. Going to prepare subnets")
		cidrValue := con.DefaultVPCCIDRValue
		if profile.ClusterConfig.NetworkingSet {
			cidrValue = clusterConfiguration.Networking.MachineCIDR
		}
		vpc, err := PrepareVPC(profile.Region, vpcPrefix, cidrValue)
		if err != nil {
			return flags, err
		}
		userData.VpcID = vpc.VpcID
		zones := strings.Split(profile.ClusterConfig.Zones, ",")
		zones = common.RemoveFromStringSlice(zones, "")
		subnets, err := PrepareSubnets(vpc, profile.Region, zones, profile.ClusterConfig.MultiAZ)
		if err != nil {
			return flags, err
		}
		subnetsFlagValue := strings.Join(append(subnets["private"], subnets["public"]...), ",")
		clusterConfiguration.Subnets = &ClusterConfigure.Subnets{
			PrivateSubnetIds: strings.Join(subnets["private"], ","),
			PublicSubnetIds:  strings.Join(subnets["public"], ","),
		}
		if profile.ClusterConfig.PrivateLink {
			log.Logger.Info("Got private link set to true. Only set private subnets to cluster flags")
			subnetsFlagValue = strings.Join(subnets["private"], ",")
			clusterConfiguration.Subnets = &ClusterConfigure.Subnets{
				PrivateSubnetIds: strings.Join(subnets["private"], ","),
			}
		}
		flags = append(flags,
			"--subnet-ids", subnetsFlagValue)

		if profile.ClusterConfig.AdditionalSGNumber != 0 {
			securityGroups, err := PrepareAdditionalSecurityGroups(vpc, profile.ClusterConfig.AdditionalSGNumber, vpcPrefix)
			if err != nil {
				return flags, err
			}
			computeSGs := strings.Join(securityGroups, ",")
			infraSGs := strings.Join(securityGroups, ",")
			controlPlaneSGs := strings.Join(securityGroups, ",")
			flags = append(flags,
				"--additional-infra-security-group-ids", infraSGs,
				"--additional-control-plane-security-group-ids", controlPlaneSGs,
				"--additional-compute-security-group-ids", computeSGs,
			)
			clusterConfiguration.AdditionalSecurityGroups = &ClusterConfigure.AdditionalSecurityGroups{
				ControlPlaneSecurityGroups: controlPlaneSGs,
				InfraSecurityGroups:        infraSGs,
				WorkerSecurityGroups:       computeSGs,
			}
		}
		if profile.ClusterConfig.ProxyEnabled {
			proxy, err := PrepareProxy(vpc, profile.Region, config.Test.ProxySSHPemFile, config.Test.ProxyCABundleFile)
			if err != nil {
				return flags, err
			}

			clusterConfiguration.Proxy = &ClusterConfigure.Proxy{
				Enabled:         profile.ClusterConfig.ProxyEnabled,
				Http:            proxy.HTTPProxy,
				Https:           proxy.HTTPsProxy,
				NoProxy:         proxy.NoProxy,
				TrustBundleFile: proxy.CABundleFilePath,
			}
			flags = append(flags,
				"--http-proxy", proxy.HTTPProxy,
				"--https-proxy", proxy.HTTPsProxy,
				"--no-proxy", proxy.NoProxy,
				"--additional-trust-bundle-file", proxy.CABundleFilePath,
			)

		}
	}
	if profile.ClusterConfig.BillingAccount != "" {
		flags = append(flags, "--billing-account", profile.ClusterConfig.BillingAccount)
		clusterConfiguration.BillingAccount = profile.ClusterConfig.BillingAccount
	}
	if profile.ClusterConfig.DisableSCPChecks {
		flags = append(flags, "--disable-scp-checks")
		clusterConfiguration.DisableScpChecks = true
	}
	if profile.ClusterConfig.DisableUserWorKloadMonitoring {
		flags = append(flags, "--disable-workload-monitoring")
		clusterConfiguration.DisableWorkloadMonitoring = true
	}
	if profile.ClusterConfig.EtcdKMS {
		keyArn, err := PrepareKMSKey(profile.Region, false, "rosacli", profile.ClusterConfig.HCP)
		userData.EtcdKMSKey = keyArn
		if err != nil {
			return flags, err
		}
		flags = append(flags,
			"--etcd-encryption-kms-arn", keyArn,
		)
		if clusterConfiguration.Encryption == nil {
			clusterConfiguration.Encryption = &ClusterConfigure.Encryption{}
		}
		clusterConfiguration.Encryption.EtcdEncryptionKmsArn = keyArn
	}

	if profile.ClusterConfig.Ec2MetadataHttpTokens != "" {
		flags = append(flags, "--ec2-metadata-http-tokens", profile.ClusterConfig.Ec2MetadataHttpTokens)
		clusterConfiguration.Ec2MetadataHttpTokens = profile.ClusterConfig.Ec2MetadataHttpTokens
	}
	if profile.ClusterConfig.EtcdEncryption {
		flags = append(flags, "--etcd-encryption")
		clusterConfiguration.EtcdEncryption = profile.ClusterConfig.EtcdEncryption

	}
	if profile.ClusterConfig.ExternalAuthConfig {
		PrepareExternalAuthConfigDummy()
	}

	if profile.ClusterConfig.FIPS {
		flags = append(flags, "--fips")
	}
	if profile.ClusterConfig.HCP {
		flags = append(flags, "--hosted-cp")
	}
	if profile.ClusterConfig.InstanceType != "" {
		flags = append(flags, "--compute-machine-type", profile.ClusterConfig.InstanceType)
	}
	if profile.ClusterConfig.KMSKey {
		kmsKeyArn, err := PrepareKMSKey(profile.Region, false, "rosacli", profile.ClusterConfig.HCP)
		userData.KMSKey = kmsKeyArn
		clusterConfiguration.Encryption = &ClusterConfigure.Encryption{
			KmsKeyArn: kmsKeyArn, // placeHolder
		}
		if err != nil {
			return flags, err
		}
		flags = append(flags,
			"--kms-key-arn", kmsKeyArn,
			"--enable-customer-managed-key",
		)
		if clusterConfiguration.Encryption == nil {
			clusterConfiguration.Encryption = &ClusterConfigure.Encryption{}
		}
		clusterConfiguration.Encryption.KmsKeyArn = kmsKeyArn
	}
	if profile.ClusterConfig.LabelEnabled {
		dmpLabel := "test-label/openshift.io=,test-label=testvalue"
		flags = append(flags, "--worker-mp-labels", dmpLabel)
		clusterConfiguration.DefaultMpLabels = dmpLabel
	}
	if profile.ClusterConfig.MultiAZ {
		flags = append(flags, "--multi-az")
		clusterConfiguration.MultiAZ = profile.ClusterConfig.MultiAZ
	}

	if profile.ClusterConfig.Private {
		flags = append(flags, "--private")
		clusterConfiguration.Private = profile.ClusterConfig.Private
	}
	if profile.ClusterConfig.PrivateLink {
		flags = append(flags, "--private-link")
		clusterConfiguration.PrivateLink = profile.ClusterConfig.PrivateLink
	}
	if profile.ClusterConfig.ProvisionShard != "" {
		flags = append(flags, "--properties", fmt.Sprintf("provision_shard_id:%s", profile.ClusterConfig.ProvisionShard))
		clusterConfiguration.Properties = &ClusterConfigure.Properties{
			ProvisionShardID: profile.ClusterConfig.ProvisionShard,
		}
	}

	if profile.ClusterConfig.SharedVPC {
		//Placeholder for shared vpc, need to research what to be set here
	}
	if profile.ClusterConfig.TagEnabled {
		tags := "test-tag:tagvalue,qe-managed:true"
		flags = append(flags, "--tags", tags)
		clusterConfiguration.Tags = tags
	}
	if profile.ClusterConfig.VolumeSize != 0 {
		diskSize := fmt.Sprintf("%dGiB", profile.ClusterConfig.VolumeSize)
		flags = append(flags, "--worker-disk-size", diskSize)
		clusterConfiguration.WorkerDiskSize = diskSize
	}
	if profile.ClusterConfig.Zones != "" && !profile.ClusterConfig.BYOVPC {
		flags = append(flags, " --availability-zones", profile.ClusterConfig.Zones)
		clusterConfiguration.AvailabilityZones = profile.ClusterConfig.Zones
	}
	if profile.ClusterConfig.ExternalAuthConfig {
		flags = append(flags, "--external-auth-providers-enabled")
	}

	return flags, nil
}
func WaitForClusterReady(client *rosacli.Client, cluster string, timeoutMin int) error {

	endTime := time.Now().Add(time.Duration(timeoutMin) * time.Minute)
	sleepTime := 0
	for time.Now().Before(endTime) {
		output, err := client.Cluster.DescribeClusterAndReflect(cluster)
		if err != nil {
			return err
		}
		switch output.State {
		case con.Ready:
			log.Logger.Infof("Cluster %s is ready now.", cluster)
			return nil
		case con.Uninstalling:
			return fmt.Errorf("cluster %s is %s now. Cannot wait for it ready",
				cluster, con.Uninstalling)
		default:
			if strings.Contains(output.State, con.Error) {
				log.Logger.Errorf("Cluster is in %s status now. Recording the installation log", con.Error)
				RecordClusterInstallationLog(client, cluster)
				return fmt.Errorf("cluster %s is in %s state with reason: %s",
					cluster, con.Error, output.State)
			}
			if strings.Contains(output.State, con.Pending) ||
				strings.Contains(output.State, con.Installing) ||
				strings.Contains(output.State, con.Validating) {
				time.Sleep(2 * time.Minute)
				continue
			}
			if strings.Contains(output.State, con.Waiting) {
				log.Logger.Infof("Cluster is in status of %v, wait for ready", con.Waiting)
				if sleepTime >= 6 {
					return fmt.Errorf("cluster stuck to %s status for more than 6 mins. Check the user data preparation for roles", output.State)
				}
				sleepTime += 2
				time.Sleep(2 * time.Minute)
				continue
			}
			return fmt.Errorf("unknown cluster state %s", output.State)
		}

	}

	return fmt.Errorf("timeout for cluster ready waiting after %d mins", timeoutMin)
}

func ReverifyClusterNetwork(client *rosacli.Client, clusterID string) error {
	log.Logger.Infof("verify network of cluster %s ", clusterID)
	_, err := client.NetworkVerifier.CreateNetworkVerifierWithCluster(clusterID)
	return err
}

func RecordClusterInstallationLog(client *rosacli.Client, cluster string) error {
	output, err := client.Cluster.InstallLog(cluster)
	if err != nil {
		return err
	}
	_, err = common.CreateFileWithContent(config.Test.ClusterInstallLogArtifactFile, output.String())
	return err
}

func CreateClusterByProfile(profile *Profile, client *rosacli.Client, waitForClusterReady bool) (*rosacli.ClusterDescription, error) {
	clusterDetail := new(ClusterDetail)

	flags, err := GenerateClusterCreateFlags(profile, client)
	if err != nil {
		log.Logger.Errorf("Error happened when generate flags: %s", err.Error())
		return nil, err
	}
	log.Logger.Infof("User data and flags preparation finished")
	_, err, createCMD := client.Cluster.Create(profile.ClusterConfig.Name, flags...)
	if err != nil {
		return nil, err
	}
	common.CreateFileWithContent(config.Test.CreateCommandFile, createCMD)
	log.Logger.Info("Cluster created succesfully")
	description, err := client.Cluster.DescribeClusterAndReflect(profile.ClusterConfig.Name)
	if err != nil {
		return nil, err
	}
	defer func() {
		log.Logger.Info("Going to record the necessary information")
		common.CreateFileWithContent(config.Test.ClusterDetailFile, clusterDetail)
		common.CreateFileWithContent(config.Test.ClusterIDFile, description.ID)          // Temporary recoding file to make it compatible to existing jobs
		common.CreateFileWithContent(config.Test.ClusterNameFile, description.Name)      // Temporary recoding file to make it compatible to existing jobs
		common.CreateFileWithContent(config.Test.APIURLFile, description.APIURL)         // Temporary recoding file to make it compatible to existing jobs
		common.CreateFileWithContent(config.Test.ConsoleUrlFile, description.ConsoleURL) // Temporary recoding file to make it compatible to existing jobs
		common.CreateFileWithContent(config.Test.InfraIDFile, description.InfraID)       // Temporary recoding file to make it compatible to existing jobs
		common.CreateFileWithContent(config.Test.ClusterTypeFile, "rosa")                // Temporary recoding file to make it compatible to existing jobs
	}()
	clusterDetail.ClusterID = description.ID
	clusterDetail.ClusterName = description.Name
	clusterDetail.ClusterType = "rosa"

	// Need to do the post step when cluster has no oidcconfig enabled
	if profile.ClusterConfig.OIDCConfig == "" {
		err = PrepareOIDCProviderByCluster(client, description.ID)
		if err != nil {
			return description, err
		}
		err = PrepareOperatorRolesByCluster(client, description.ID)
		if err != nil {
			return description, err
		}

	}
	// Need to decorate the KMS key
	if profile.ClusterConfig.KMSKey {
		err = ElaborateKMSKeyForSTSCluster(client, description.ID, false)
		if err != nil {
			return description, err
		}
	}
	if profile.ClusterConfig.EtcdKMS {
		err = ElaborateKMSKeyForSTSCluster(client, description.ID, true)
		if err != nil {
			return description, err
		}
	}
	// if profile.ClusterConfig.BYOVPC {
	// log.Logger.Infof("Reverify the network for the cluster %s to make sure it can be parsed", description.ID)
	// 	ReverifyClusterNetwork(client, description.ID)
	// }
	if waitForClusterReady {
		log.Logger.Infof("Waiting for the cluster %s to ready", description.ID)
		err = WaitForClusterReady(client, description.ID, config.Test.GlobalENV.ClusterWaitingTime)
		if err != nil {
			return description, err
		}
		description, err = client.Cluster.DescribeClusterAndReflect(profile.ClusterConfig.Name)
	}
	return description, err
}
