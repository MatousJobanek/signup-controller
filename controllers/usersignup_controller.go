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
	"time"

	"github.com/codeready-toolchain/api/api/v1alpha1"
	commonCondition "github.com/codeready-toolchain/toolchain-common/pkg/condition"
	"github.com/codeready-toolchain/toolchain-common/pkg/states"
	v1alpha12 "github.com/kcp-dev/kcp/pkg/apis/tenancy/v1alpha1"
	tenancyv1beta1 "github.com/kcp-dev/kcp/pkg/apis/tenancy/v1beta1"
	"github.com/redhat-cop/operator-utils/pkg/util"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// UserSignupReconciler reconciles a ClusterWorkspace object
type UserSignupReconciler struct {
	Client     client.Client
	Scheme     *runtime.Scheme
	OrgCluster cluster.Cluster
	Namespace  string
}

// SetupWithManager sets up the controller with the Manager.
func (r *UserSignupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		// Uncomment the following line adding a pointer to an instance of the controlled resource as an argument
		For(&v1alpha1.UserSignup{}).
		Watches(source.NewKindWithCache(&v1alpha12.ClusterWorkspace{}, r.OrgCluster.GetCache()),
			handler.EnqueueRequestsFromMapFunc(MapToOwnerByLabel("usersignup-namespace", "usersignup"))).
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
	if !states.Approved(userSignup) {
		logger.Info("user is not approved")
		return reconcile.Result{}, nil
	}
	ssoUsername := fmt.Sprintf("rh-sso:%s", userSignup.Spec.Username)

	if util.IsBeingDeleted(userSignup) {
		logger.Info("usersignup is being deleted")
		deleted, err := r.deleteClusterWorkspace(logger, userSignup)
		if deleted || err != nil {
			return ctrl.Result{
				Requeue:      true,
				RequeueAfter: time.Second,
			}, err
		}
		util.RemoveFinalizer(userSignup, v1alpha1.FinalizerName)
		if err := r.Client.Update(context.TODO(), userSignup); err != nil {
			return ctrl.Result{}, err
		}
	}

	if !util.HasFinalizer(userSignup, v1alpha1.FinalizerName) {
		util.AddFinalizer(userSignup, v1alpha1.FinalizerName)
		if err := r.Client.Update(context.TODO(), userSignup); err != nil {
			return ctrl.Result{}, err
		}
	}

	workspace, created, err := r.ensureClusterWorkspace(logger, userSignup)
	if created || err != nil {
		return ctrl.Result{}, err
	}

	if err := r.ensureRole(logger, workspace); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.ensureRoleBinding(logger, ssoUsername, workspace); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.ensureRoleBinding(logger, "rh-sso:mjobanek-stage-kcp", workspace); err != nil {
		return ctrl.Result{}, err
	}

	if workspace.Status.Phase != v1alpha12.ClusterWorkspacePhaseReady || workspace.Status.BaseURL == "" {
		logger.Info("workspace is not ready yet")
		return ctrl.Result{
			Requeue:      true,
			RequeueAfter: time.Second,
		}, nil
	}
	logger.Info("workspace is ready")
	return ctrl.Result{}, updateCompleteStatus(r.Client, userSignup, workspace.Status.BaseURL)
}

func (r *UserSignupReconciler) deleteClusterWorkspace(logger klog.Logger, userSignup *v1alpha1.UserSignup) (bool, error) {
	logger.Info("getting clusterworkspace")
	workspace := &v1alpha12.ClusterWorkspace{}
	err := r.OrgCluster.GetClient().Get(context.TODO(), types.NamespacedName{Name: userSignup.Name}, workspace)
	if err != nil {
		if !errors.IsNotFound(err) {
			return false, err
		}
		logger.Info("ClusterWorkspace is gone")
		return false, nil
	}
	if util.IsBeingDeleted(workspace) {
		logger.Info("ClusterWorkspace is being deleted")
		return false, nil
	}
	logger.Info("ClusterWorkspace was deleted")
	return true, r.OrgCluster.GetClient().Delete(context.TODO(), workspace)
}

func (r *UserSignupReconciler) ensureClusterWorkspace(logger klog.Logger, userSignup *v1alpha1.UserSignup) (*v1alpha12.ClusterWorkspace, bool, error) {
	logger.Info("getting clusterworkspace")
	workspace := &v1alpha12.ClusterWorkspace{}
	err := r.OrgCluster.GetClient().Get(context.TODO(), types.NamespacedName{Name: userSignup.Name}, workspace)
	if err != nil {
		if !errors.IsNotFound(err) {
			return nil, false, err
		}
		logger.Info("clusterworkspace not found")
		workspace = &v1alpha12.ClusterWorkspace{
			ObjectMeta: metav1.ObjectMeta{
				Name: userSignup.Name,
				Labels: map[string]string{
					"type":                          "appstudio",
					"usersignup-name":               userSignup.Name,
					"usersignup-namespace":          userSignup.Namespace,
					"workloads.kcp.dev/schedulable": "true",
				},
			},
			Spec: v1alpha12.ClusterWorkspaceSpec{
				Type: "Universal",
			},
		}
		if err := r.OrgCluster.GetClient().Create(context.TODO(), workspace); err != nil {
			return nil, false, err
		}
		logger.Info("clusterworkspace created")
		return workspace, true, nil
	}
	return workspace, false, nil
}

func (r *UserSignupReconciler) ensureRole(logger klog.Logger, workspace *v1alpha12.ClusterWorkspace) error {
	roleName := fmt.Sprintf("%s-owner", workspace.Name)
	logger.Info("getting clusterrole", "name", roleName)
	clusterRole := &rbacv1.ClusterRole{}
	if err := r.OrgCluster.GetClient().Get(context.TODO(), types.NamespacedName{Name: roleName}, clusterRole); err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
		logger.Info("clusterrole not found")
		clusterRole = createClusterRole(roleName, workspace.Name, OwnerRoleType)
		if err := r.OrgCluster.GetClient().Create(context.TODO(), clusterRole); err != nil {
			return err
		}
		logger.Info("clusterrole created")
	}
	if err := controllerutil.SetOwnerReference(workspace, clusterRole, r.Scheme); err != nil {
		return err
	}
	if err := r.OrgCluster.GetClient().Update(context.TODO(), clusterRole); err != nil {
		return err
	}
	return nil
}

func (r *UserSignupReconciler) ensureRoleBinding(logger klog.Logger, ssoUsername string, workspace *v1alpha12.ClusterWorkspace) error {
	ownerRoleBindingName := getRoleBindingName(OwnerRoleType, workspace.Name, ssoUsername)
	roleName := fmt.Sprintf("%s-owner", workspace.Name)

	logger.Info("getting clusterrolebinding", "name", ownerRoleBindingName)
	clusterRoleBinding := &rbacv1.ClusterRoleBinding{}
	if err := r.OrgCluster.GetClient().Get(context.TODO(), types.NamespacedName{Name: ownerRoleBindingName}, clusterRoleBinding); err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
		logger.Info("clusterrolebinding not found")
		clusterRoleBinding := &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: ownerRoleBindingName,
				Labels: map[string]string{
					"workspaces.kcp.dev/internal-name": workspace.Name,
					"workspaces.kcp.dev/pretty-name":   workspace.Name,
				},
			},
			RoleRef: rbacv1.RoleRef{
				Kind:     "ClusterRole",
				APIGroup: "rbac.authorization.k8s.io",
				Name:     roleName,
			},
			Subjects: []rbacv1.Subject{
				{
					Kind:      "User",
					Name:      ssoUsername,
					Namespace: "",
				},
			},
		}
		if err := r.OrgCluster.GetClient().Create(context.TODO(), clusterRoleBinding); err != nil {
			return err
		}
		logger.Info("clusterrolebinding created")
	}
	if err := controllerutil.SetOwnerReference(workspace, clusterRoleBinding, r.Scheme); err != nil {
		return err
	}
	if err := r.OrgCluster.GetClient().Update(context.TODO(), clusterRoleBinding); err != nil {
		return err
	}
	return nil
}

func getRoleBindingName(roleType RoleType, workspacePrettyName string, userName string) string {
	return string(roleType) + "-workspace-" + workspacePrettyName + "-" + userName
}

type RoleType string

const (
	OwnerRoleType RoleType = "owner"
)

var roleRules = map[RoleType][]rbacv1.PolicyRule{
	OwnerRoleType: {
		{
			Verbs:     []string{"get", "delete"},
			Resources: []string{"clusterworkspaces/workspace"},
		},
		{
			Resources: []string{"clusterworkspaces/content"},
			Verbs:     []string{"admin", "access"},
		},
	},
}

func createClusterRole(name, workspaceName string, roleType RoleType) *rbacv1.ClusterRole {
	var rules []rbacv1.PolicyRule
	for _, rule := range roleRules[roleType] {
		rule.APIGroups = []string{tenancyv1beta1.SchemeGroupVersion.Group}
		rule.ResourceNames = []string{workspaceName}
		rules = append(rules, rule)
	}
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"workspaces.kcp.dev/internal-name": workspaceName,
			},
		},
		Rules: rules,
	}
}

func updateCompleteStatus(cl client.Client, userSignup *v1alpha1.UserSignup, url string) error {

	var conditionUpdated bool
	userSignup.Status.Conditions, conditionUpdated = commonCondition.AddOrUpdateStatusConditions(userSignup.Status.Conditions,
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

	if !conditionUpdated {
		// Nothing changed
		return nil
	}

	return cl.Status().Update(context.TODO(), userSignup)
}

func MapToOwnerByLabel(namespaceLabel, nameLabel string) func(object client.Object) []reconcile.Request {
	return func(obj client.Object) []reconcile.Request {
		if name, exists := obj.GetLabels()[nameLabel]; exists {
			namespace := ""
			if nameLabel != "" {
				if namespace, exists = obj.GetLabels()[namespaceLabel]; !exists {
					namespace = "toolchain-host-operator"
				}
			}
			return []reconcile.Request{
				{
					NamespacedName: types.NamespacedName{
						Namespace: namespace,
						Name:      name,
					},
				},
			}
		}
		// the obj was not a namespace or it did not have the required label.
		return []reconcile.Request{}
	}
}
