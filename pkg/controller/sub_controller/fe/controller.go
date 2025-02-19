package fe

import (
	"context"
	v1 "github.com/selectdb/doris-operator/api/doris/v1"
	"github.com/selectdb/doris-operator/pkg/common/utils/k8s"
	"github.com/selectdb/doris-operator/pkg/common/utils/resource"
	"github.com/selectdb/doris-operator/pkg/controller/sub_controller"
	appv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"time"
)

type Controller struct {
	sub_controller.SubDefaultController
}

func (fc *Controller) ClearResources(ctx context.Context, cluster *v1.DorisCluster) (bool, error) {
	//if the doris is not have fe.
	if cluster.Status.FEStatus == nil {
		return true, nil
	}

	if cluster.DeletionTimestamp.IsZero() {
		return true, nil
	}

	return fc.ClearCommonResources(ctx, cluster, v1.Component_FE)
}

func (fc *Controller) UpdateComponentStatus(cluster *v1.DorisCluster) error {
	//if spec is not exist, status is empty. but before clear status we must clear all resource about be used by ClearResources.
	if cluster.Spec.FeSpec == nil {
		cluster.Status.FEStatus = nil
		return nil
	}

	fs := &v1.ComponentStatus{
		ComponentCondition: v1.ComponentCondition{
			SubResourceName:    v1.GenerateComponentStatefulSetName(cluster, v1.Component_FE),
			Phase:              v1.Reconciling,
			LastTransitionTime: metav1.NewTime(time.Now()),
		},
	}

	if cluster.Status.FEStatus != nil {
		fs = cluster.Status.FEStatus.DeepCopy()
	}

	cluster.Status.FEStatus = fs
	fs.AccessService = v1.GenerateExternalServiceName(cluster, v1.Component_FE)

	return fc.ClassifyPodsByStatus(cluster.Namespace, fs, v1.GenerateStatefulSetSelector(cluster, v1.Component_FE), *cluster.Spec.FeSpec.Replicas)
}

// New construct a FeController.
func New(k8sclient client.Client, k8sRecorder record.EventRecorder) *Controller {
	return &Controller{
		SubDefaultController: sub_controller.SubDefaultController{
			K8sclient:   k8sclient,
			K8srecorder: k8sRecorder,
		},
	}
}

func (fc *Controller) GetControllerName() string {
	return "feController"
}

// Sync DorisCluster to fe statefulset and service.
func (fc *Controller) Sync(ctx context.Context, cluster *v1.DorisCluster) error {
	if cluster.Spec.FeSpec == nil {
		klog.Info("fe Controller Sync ", "the fe component is not needed ", "namespace ", cluster.Namespace, " doris cluster name ", cluster.Name)
		return nil
	}

	feSpec := cluster.Spec.FeSpec
	//get the fe configMap for resolve ports.
	config, err := fc.GetConfig(ctx, &feSpec.BaseSpec.ConfigMapInfo, cluster.Namespace)
	if err != nil {
		klog.Error("fe Controller Sync ", "resolve fe configmap failed, namespace ", cluster.Namespace, " configmapName ", feSpec.BaseSpec.ConfigMapInfo.ConfigMapName, " configMapKey ", feSpec.ConfigMapInfo.ResolveKey, " error ", err)
		return err
	}

	//generate new fe service.
	svc := resource.BuildExternalService(cluster, v1.Component_FE, config)
	//create or update fe external and domain search service, update the status of fe on src.
	internalService := resource.BuildInternalService(cluster, v1.Component_FE, config)
	if err := k8s.ApplyService(ctx, fc.K8sclient, &internalService, resource.ServiceDeepEqual); err != nil {
		klog.Errorf("fe controller sync apply internalService name=%s, namespace=%s, clusterName=%s failed.message=%s.",
			internalService.Name, internalService.Namespace, cluster.Name, err.Error())
		return err
	}
	if err := k8s.ApplyService(ctx, fc.K8sclient, &svc, resource.ServiceDeepEqual); err != nil {
		klog.Errorf("fe controller sync apply external service name=%s, namespace=%s, clusterName=%s failed. message=%s.",
			svc.Name, svc.Namespace, cluster.Name, err.Error())
		return err
	}

	st := fc.buildFEStatefulSet(cluster)
	if err = k8s.ApplyStatefulSet(ctx, fc.K8sclient, &st, func(new *appv1.StatefulSet, est *appv1.StatefulSet) bool {
		// if have restart annotation, we should exclude the interference for comparison.
		return resource.StatefulSetDeepEqual(new, est, false)
	}); err != nil {
		klog.Errorf("fe controller sync statefulset name=%s, namespace=%s, clusterName=%s failed. message=%s.",
			st.Name, st.Namespace)
		return err
	}

	return nil
}
