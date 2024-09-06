package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	mapiv1 "github.com/openshift/api/machine/v1beta1"
	"github.com/openshift/cluster-capi-operator/e2e/framework"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ptr "k8s.io/utils/ptr"
	azurev1 "sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	yaml "sigs.k8s.io/yaml"
)

const (
	azureMachineTemplateName        = "azure-machine-template"
	clusterSecretName               = "capz-manager-cluster-credential"
	capzManagerBootstrapCredentials = "capz-manager-bootstrap-credentials"
)

var _ = Describe("Cluster API Azure MachineSet", Ordered, func() {
	var azureMachineTemplate *azurev1.AzureMachineTemplate
	var machineSet *clusterv1.MachineSet
	var mapiMachineSpec *mapiv1.AzureMachineProviderSpec

	BeforeAll(func() {
		if platform != configv1.AzurePlatformType {
			Skip("Skipping Azure E2E tests")
		}
		framework.CreateCoreCluster(cl, clusterName, "AzureCluster")
		mapiMachineSpec = getAzureMAPIProviderSpec(cl)
		createAzureCluster(cl, mapiMachineSpec)
	})

	AfterEach(func() {
		if platform != configv1.AzurePlatformType {
			// Because AfterEach always runs, even when tests are skipped, we have to
			// explicitly skip it here for other platforms.
			Skip("Skipping Azure E2E tests")
		}
		framework.DeleteMachineSets(cl, machineSet)
		framework.WaitForMachineSetsDeleted(cl, machineSet)
		framework.DeleteObjects(cl, azureMachineTemplate)
	})

	It("should be able to run a machine", func() {
		azureMachineTemplate = createAzureMachineTemplate(cl, mapiMachineSpec)

		machineSet = framework.CreateMachineSet(cl, framework.NewMachineSetParams(
			"azure-machineset",
			clusterName,
			"",
			1,
			corev1.ObjectReference{
				Kind:       "AzureMachineTemplate",
				APIVersion: infraAPIVersion,
				Name:       azureMachineTemplateName,
			},
		))

		framework.WaitForMachineSet(cl, machineSet.Name)
	})

})

func getAzureMAPIProviderSpec(cl client.Client) *mapiv1.AzureMachineProviderSpec {
	machineSetList := &mapiv1.MachineSetList{}
	Expect(cl.List(ctx, machineSetList, client.InNamespace(framework.MAPINamespace))).To(Succeed())

	Expect(machineSetList.Items).ToNot(HaveLen(0))
	machineSet := machineSetList.Items[0]
	Expect(machineSet.Spec.Template.Spec.ProviderSpec.Value).ToNot(BeNil())

	providerSpec := &mapiv1.AzureMachineProviderSpec{}
	Expect(yaml.Unmarshal(machineSet.Spec.Template.Spec.ProviderSpec.Value.Raw, providerSpec)).To(Succeed())

	return providerSpec
}

func createAzureCluster(cl client.Client, mapiProviderSpec *mapiv1.AzureMachineProviderSpec) *azurev1.AzureCluster {
	By("Creating Azure cluster secret")
	capzManagerBootstrapCredentialsKey := client.ObjectKey{Namespace: framework.CAPINamespace, Name: capzManagerBootstrapCredentials}
	capzManagerBootstrapCredentials := &corev1.Secret{}

	if err := cl.Get(ctx, capzManagerBootstrapCredentialsKey, capzManagerBootstrapCredentials); err != nil {
		Expect(err).ToNot(HaveOccurred())
	}

	azureClientSecret, found := capzManagerBootstrapCredentials.Data["azure_client_secret"]
	Expect(found).To(BeTrue())

	azureSecretKey := corev1.SecretReference{Name: clusterSecretName, Namespace: framework.CAPINamespace}
	azureSecret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      azureSecretKey.Name,
			Namespace: azureSecretKey.Namespace,
		},
		Immutable: ptr.To(true),
		Data: map[string][]byte{
			"clientSecret": azureClientSecret,
		},
	}

	if err := cl.Create(ctx, &azureSecret); err != nil && !apierrors.IsAlreadyExists(err) {
		Expect(err).ToNot(HaveOccurred())
	}
	By("Creating Azure cluster identity")

	var azureClientID, azureTenantID []byte
	azureClientID, found = capzManagerBootstrapCredentials.Data["azure_client_id"]
	Expect(found).To(BeTrue())
	azureTenantID, found = capzManagerBootstrapCredentials.Data["azure_tenant_id"]
	Expect(found).To(BeTrue())

	azureClusterIdentity := &azurev1.AzureClusterIdentity{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: framework.CAPINamespace,
		},
		Spec: azurev1.AzureClusterIdentitySpec{
			Type:              azurev1.ServicePrincipal,
			AllowedNamespaces: &azurev1.AllowedNamespaces{NamespaceList: []string{framework.CAPINamespace}},
			ClientID:          string(azureClientID),
			TenantID:          string(azureTenantID),
			ClientSecret:      corev1.SecretReference{Name: clusterSecretName, Namespace: framework.CAPINamespace},
		},
	}

	if err := cl.Create(ctx, azureClusterIdentity); err != nil && !apierrors.IsAlreadyExists(err) {
		Expect(err).ToNot(HaveOccurred())
	}

	By("Creating Azure cluster")
	azureCluster := &azurev1.AzureCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: framework.CAPINamespace,
			Annotations: map[string]string{
				// The ManagedBy Annotation is set so CAPI infra providers ignore the InfraCluster object,
				// as that's managed externally, in this case by the cluster-capi-operator's infracluster controller.
				clusterv1.ManagedByAnnotation: managedByAnnotationValueClusterCAPIOperatorInfraClusterController,
			},
		},
		Spec: azurev1.AzureClusterSpec{
			AzureClusterClassSpec: azurev1.AzureClusterClassSpec{
				Location:         mapiProviderSpec.Location,
				AzureEnvironment: "AzurePublicCloud",
				IdentityRef: &corev1.ObjectReference{
					Name:      clusterName,
					Namespace: framework.CAPINamespace,
					Kind:      "AzureClusterIdentity",
				},
			},
			NetworkSpec: azurev1.NetworkSpec{
				NodeOutboundLB: &azurev1.LoadBalancerSpec{
					Name: clusterName,
					BackendPool: azurev1.BackendPool{
						Name: clusterName,
					},
				},
				Vnet: azurev1.VnetSpec{
					Name:          mapiProviderSpec.Vnet,
					ResourceGroup: mapiProviderSpec.NetworkResourceGroup,
				},
			},
			ResourceGroup: mapiProviderSpec.ResourceGroup,
		},
	}

	if err := cl.Create(ctx, azureCluster); err != nil && !apierrors.IsAlreadyExists(err) {
		Expect(err).ToNot(HaveOccurred())
	}

	Eventually(func() (bool, error) {
		patchedAzureCluster := &azurev1.AzureCluster{}
		err := cl.Get(ctx, client.ObjectKeyFromObject(azureCluster), patchedAzureCluster)
		if err != nil {
			return false, err
		}

		if patchedAzureCluster.Annotations == nil {
			return false, nil
		}

		if _, ok := patchedAzureCluster.Annotations[clusterv1.ManagedByAnnotation]; !ok {
			return false, nil
		}

		return patchedAzureCluster.Status.Ready, nil
	}, framework.WaitShort).Should(BeTrue())

	return azureCluster
}

func createAzureMachineTemplate(cl client.Client, mapiProviderSpec *mapiv1.AzureMachineProviderSpec) *azurev1.AzureMachineTemplate {
	By("Creating Azure machine template")

	Expect(mapiProviderSpec).ToNot(BeNil())
	Expect(mapiProviderSpec.Subnet).ToNot(BeEmpty())
	Expect(mapiProviderSpec.AcceleratedNetworking).ToNot(BeNil())
	Expect(mapiProviderSpec.Image.ResourceID).ToNot(BeEmpty())
	Expect(mapiProviderSpec.OSDisk.ManagedDisk.StorageAccountType).ToNot(BeEmpty())
	Expect(mapiProviderSpec.OSDisk.DiskSizeGB).To(BeNumerically(">", 0))
	Expect(mapiProviderSpec.OSDisk.OSType).ToNot(BeEmpty())
	Expect(mapiProviderSpec.VMSize).ToNot(BeEmpty())

	azure_credentials_secret := corev1.Secret{}
	azure_credentials_secret_key := types.NamespacedName{Name: "capz-manager-bootstrap-credentials", Namespace: "openshift-cluster-api"}
	err := cl.Get(context.Background(), azure_credentials_secret_key, &azure_credentials_secret)
	Expect(err).To(BeNil(), "capz-manager-bootstrap-credentials secret should exist")
	subscriptionID := azure_credentials_secret.Data["azure_subscription_id"]
	azureImageID := fmt.Sprintf("/subscriptions/%s%s", subscriptionID, mapiProviderSpec.Image.ResourceID)
	azureMachineSpec := azurev1.AzureMachineSpec{
		Identity: azurev1.VMIdentityUserAssigned,
		UserAssignedIdentities: []azurev1.UserAssignedIdentity{
			{
				ProviderID: fmt.Sprintf("azure:///subscriptions/%s/resourcegroups/%s/providers/Microsoft.ManagedIdentity/userAssignedIdentities/%s", subscriptionID, mapiProviderSpec.ResourceGroup, mapiProviderSpec.ManagedIdentity),
			},
		},
		NetworkInterfaces: []azurev1.NetworkInterface{
			{
				PrivateIPConfigs:      1,
				SubnetName:            mapiProviderSpec.Subnet,
				AcceleratedNetworking: &mapiProviderSpec.AcceleratedNetworking,
			},
		},
		Image: &azurev1.Image{
			ID: &azureImageID,
		},
		OSDisk: azurev1.OSDisk{
			DiskSizeGB: &mapiProviderSpec.OSDisk.DiskSizeGB,
			ManagedDisk: &azurev1.ManagedDiskParameters{
				StorageAccountType: mapiProviderSpec.OSDisk.ManagedDisk.StorageAccountType,
			},
			CachingType: mapiProviderSpec.OSDisk.CachingType,
			OSType:      mapiProviderSpec.OSDisk.OSType,
		},
		DisableExtensionOperations: ptr.To(true),
		SSHPublicKey:               mapiProviderSpec.SSHPublicKey,
		VMSize:                     mapiProviderSpec.VMSize,
	}

	azureMachineTemplate := &azurev1.AzureMachineTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      azureMachineTemplateName,
			Namespace: framework.CAPINamespace,
		},
		Spec: azurev1.AzureMachineTemplateSpec{
			Template: azurev1.AzureMachineTemplateResource{
				Spec: azureMachineSpec,
			},
		},
	}

	if err := cl.Create(ctx, azureMachineTemplate); err != nil && !apierrors.IsAlreadyExists(err) {
		Expect(err).ToNot(HaveOccurred())
	}

	return azureMachineTemplate
}