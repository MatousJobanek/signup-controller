/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"

	"github.com/codeready-toolchain/api/api/v1alpha1"
	commonCondition "github.com/codeready-toolchain/toolchain-common/pkg/condition"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// UserSignupReconciler reconciles a ClusterWorkspace object
type UserSignupReconciler struct {
	Client client.Client
	Scheme *runtime.Scheme
	Host   string
}

// SetupWithManager sets up the controller with the Manager.
func (r *UserSignupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		// Uncomment the following line adding a pointer to an instance of the controlled resource as an argument
		For(&v1alpha1.UserSignup{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		//Watches(source.NewKindWithCache(&v1alpha12.ClusterWorkspace{}, r.OrgCluster.GetCache()),
		//	handler.EnqueueRequestsFromMapFunc(MapToOwnerByLabel("usersignup-namespace", "usersignup"))).
		Complete(r)
}

//+kubebuilder:rbac:groups=toolchain.dev.openshift.com,resources=usersignups,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=toolchain.dev.openshift.com,resources=usersignups/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=toolchain.dev.openshift.com,resources=usersignups/finalizers,verbs=update

//+kubebuilder:rbac:groups=tenancy.kcp.dev,resources=clusterworkspaces,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=tenancy.kcp.dev,resources=clusterworkspaces/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=tenancy.kcp.dev,resources=clusterworkspaces/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ClusterWorkspace object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (r *UserSignupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling UserSignup")

	userSignup := &v1alpha1.UserSignup{}
	err := r.Client.Get(context.TODO(), req.NamespacedName, userSignup)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			logger.Info("not found")
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}
	logger = logger.WithValues("username", userSignup.Spec.Username)

	approved := false
	statuses := []v1alpha1.ServiceRequestStatus{}
	for _, request := range userSignup.Spec.ServiceRequests {
		var conds []v1alpha1.Condition
		if request.Approved {
			logger.Info("user is approved for service", "serviceName", request.ServiceName)
			conds, _ = commonCondition.AddOrUpdateStatusConditions(conds, v1alpha1.Condition{
				Type:    v1alpha1.UserSignupApproved,
				Status:  corev1.ConditionTrue,
				Reason:  "",
				Message: "N/A",
			})
			if request.ServiceName == v1alpha1.HomeWorkspace {
				approved = true
			}
		} else {
			logger.Info("user is not approved for service", "serviceName", request.ServiceName)
			conds, _ = commonCondition.AddOrUpdateStatusConditions(conds, v1alpha1.Condition{
				Type:    v1alpha1.UserSignupApproved,
				Status:  corev1.ConditionFalse,
				Reason:  v1alpha1.UserSignupPendingApprovalReason,
				Message: "",
			})
		}
		statuses = append(statuses, v1alpha1.ServiceRequestStatus{
			ServiceName: request.ServiceName,
			Conditions:  conds,
		})
	}
	userSignup.Status.ServiceRequestStatuses = statuses
	//if err := r.Client.Status().Update(context.TODO(), userSignup); err != nil {
	//	return ctrl.Result{}, err
	//}
	if !approved {
		logger.Info("user is not approved for any service")
		return ctrl.Result{}, updateIncompleteStatus(r.Client, userSignup)
	}

	logger.Info("workspace is ready")
	return ctrl.Result{}, updateCompleteStatus(r.Client, userSignup, fmt.Sprintf("%s/clusters/root/apis/tenancy.kcp.dev/v1beta1/workspaces/~", r.Host))
}

func updateIncompleteStatus(cl client.Client, userSignup *v1alpha1.UserSignup) error {

	//var conditionUpdated bool
	userSignup.Status.Conditions, _ = commonCondition.AddOrUpdateStatusConditions(userSignup.Status.Conditions,
		v1alpha1.Condition{
			Type:    v1alpha1.UserSignupComplete,
			Status:  corev1.ConditionFalse,
			Reason:  "",
			Message: "",
		},
		v1alpha1.Condition{
			Type:    v1alpha1.UserSignupApproved,
			Status:  corev1.ConditionFalse,
			Reason:  v1alpha1.UserSignupPendingApprovalReason,
			Message: "",
		})

	//if !conditionUpdated {
	//	// Nothing changed
	//	return nil
	//}

	return cl.Status().Update(context.TODO(), userSignup)
}

func updateCompleteStatus(cl client.Client, userSignup *v1alpha1.UserSignup, url string) error {

	//var conditionUpdated bool
	userSignup.Status.Conditions, _ = commonCondition.AddOrUpdateStatusConditions(userSignup.Status.Conditions,
		v1alpha1.Condition{
			Type:    v1alpha1.UserSignupComplete,
			Status:  corev1.ConditionTrue,
			Reason:  "",
			Message: url,
		},
		v1alpha1.Condition{
			Type:    v1alpha1.UserSignupApproved,
			Status:  corev1.ConditionTrue,
			Reason:  v1alpha1.UserSignupApprovedByAdminReason,
			Message: "",
		})

	//if !conditionUpdated {
	//	// Nothing changed
	//	return nil
	//}

	return cl.Status().Update(context.TODO(), userSignup)
}
