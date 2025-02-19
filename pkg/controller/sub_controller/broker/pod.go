package broker

import (
	"context"
	v1 "github.com/selectdb/doris-operator/api/doris/v1"
	"github.com/selectdb/doris-operator/pkg/common/utils/resource"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"strconv"
)

func (broker *Controller) buildBrokerPodTemplateSpec(dcr *v1.DorisCluster) corev1.PodTemplateSpec {
	podTemplateSpec := resource.NewPodTemplateSpec(dcr, v1.Component_Broker)
	var containers []corev1.Container
	broker.brokerAffinity(dcr, &podTemplateSpec)
	containers = append(containers, podTemplateSpec.Spec.Containers...)
	bkContainer := broker.brokerContainer(dcr)
	containers = append(containers, bkContainer)
	podTemplateSpec.Spec.Containers = containers
	return podTemplateSpec
}

func (broker *Controller) brokerAffinity(dcr *v1.DorisCluster, podTemplateSpec *corev1.PodTemplateSpec) {
	defaultAffinity := broker.getDefaultBorkerPodAffinity()
	podTemplateSpec.Spec.Affinity = dcr.Spec.BrokerSpec.Affinity
	/*
		kickOffAffinityBe	BrokerSpec.Affinity==nil		AffinityRule in effect
					true				true					defaultAffinity(bk affinity be)
					false				true					User-defined Affinity(User-defined in dcr)
					true				false					User-defined Affinity(User-defined in dcr)
					false				false					User-defined Affinity(User-defined in dcr)
	*/
	if dcr.Spec.BrokerSpec.Affinity == nil && dcr.Spec.BrokerSpec.KickOffAffinityBe {
		podTemplateSpec.Spec.Affinity = defaultAffinity
	}
}

func (broker *Controller) brokerContainer(dcr *v1.DorisCluster) corev1.Container {
	config, _ := broker.GetConfig(context.Background(), &dcr.Spec.BrokerSpec.ConfigMapInfo, dcr.Namespace)
	c := resource.NewBaseMainContainer(dcr, config, v1.Component_Broker)
	addr, port := v1.GetConfigFEAddrForAccess(dcr, v1.Component_Broker)
	var feConfig map[string]interface{}
	//if fe addr not config, we should use external service as addr and port get from fe config.
	if addr == "" {
		if dcr.Spec.FeSpec != nil {
			feConfig, _ = broker.getFeConfig(context.Background(), &dcr.Spec.FeSpec.ConfigMapInfo, dcr.Namespace)
		}

		addr = v1.GenerateExternalServiceName(dcr, v1.Component_FE)
	}

	feQueryPort := strconv.FormatInt(int64(resource.GetPort(feConfig, resource.QUERY_PORT)), 10)
	if port != -1 {
		feQueryPort = strconv.FormatInt(int64(port), 10)
	}

	ports := resource.GetContainerPorts(config, v1.Component_Broker)
	c.Name = "broker"
	c.Ports = append(c.Ports, ports...)
	c.Env = append(c.Env, corev1.EnvVar{
		Name:  resource.ENV_FE_ADDR,
		Value: addr,
	}, corev1.EnvVar{
		Name:  resource.ENV_FE_PORT,
		Value: feQueryPort,
	})

	return c
}

// the broker Pod Affinity rule
func (broker *Controller) getDefaultBorkerPodAffinity() *corev1.Affinity {

	podAffinityTerm := corev1.WeightedPodAffinityTerm{
		Weight: 100,
		PodAffinityTerm: corev1.PodAffinityTerm{
			LabelSelector: &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{Key: v1.ComponentLabelKey, Operator: metav1.LabelSelectorOpIn, Values: []string{string(v1.Component_BE)}},
				},
			},
			TopologyKey: "kubernetes.io/hostname",
		},
	}

	affinity := corev1.PodAffinity{
		PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{podAffinityTerm},
	}

	return &corev1.Affinity{
		PodAffinity: &affinity,
	}

}
