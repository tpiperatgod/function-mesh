// Licensed to the Apache Software Foundation (ASF) under one
// or more contributor license agreements.  See the NOTICE file
// distributed with this work for additional information
// regarding copyright ownership.  The ASF licenses this file
// to you under the Apache License, Version 2.0 (the
// "License"); you may not use this file except in compliance
// with the License.  You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package controllers

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	autov2beta2 "k8s.io/api/autoscaling/v2beta2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/streamnative/function-mesh/api/compute/v1alpha1"
	"github.com/streamnative/function-mesh/controllers/spec"
)

func (r *FunctionReconciler) ObserveFunctionStatefulSet(ctx context.Context, function *v1alpha1.Function) error {
	statefulSet := &appsv1.StatefulSet{}
	err := r.Get(ctx, types.NamespacedName{
		Namespace: function.Namespace,
		Name:      spec.MakeFunctionObjectMeta(function).Name,
	}, statefulSet)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("function statefulSet is not ready yet...",
				"namespace", function.Namespace, "name", function.Name,
				"statefulSet name", statefulSet.Name)
			return nil
		}
		function.SetCondition(v1alpha1.Error, metav1.ConditionTrue, v1alpha1.StatefulSetError,
			fmt.Sprintf("error fetching function statefulSet: %v", err))
		return err
	}

	selector, err := metav1.LabelSelectorAsSelector(statefulSet.Spec.Selector)
	if err != nil {
		r.Log.Error(err, "error retrieving statefulSet selector")
		function.SetCondition(v1alpha1.Error, metav1.ConditionTrue, v1alpha1.StatefulSetError,
			fmt.Sprintf("error retrieving statefulSet selector: %v", err))
		return err
	}
	function.Status.Selector = selector.String()

	if statefulSet.Status.ReadyReplicas == *function.Spec.Replicas {
		function.SetCondition(v1alpha1.StatefulSetReady, metav1.ConditionTrue, v1alpha1.StatefulSetIsReady, "")
	} else {
		function.SetCondition(v1alpha1.StatefulSetReady, metav1.ConditionFalse, v1alpha1.PendingCreation,
			"wait for the number of replicas of statefulSet to be ready")
	}
	function.Status.Replicas = *statefulSet.Spec.Replicas
	return nil
}

func (r *FunctionReconciler) ApplyFunctionStatefulSet(ctx context.Context, function *v1alpha1.Function) error {
	if !r.checkIfStatefulSetNeedUpdate(function) {
		return nil
	}
	desiredStatefulSet := spec.MakeFunctionStatefulSet(function)
	desiredStatefulSetSpec := desiredStatefulSet.Spec
	if _, err := ctrl.CreateOrUpdate(ctx, r.Client, desiredStatefulSet, func() error {
		// function statefulSet mutate logic
		desiredStatefulSet.Spec = desiredStatefulSetSpec
		return nil
	}); err != nil {
		r.Log.Error(err, "error creating or updating statefulSet for function",
			"namespace", function.Namespace, "name", function.Name,
			"statefulSet name", desiredStatefulSet.Name)
		function.SetCondition(v1alpha1.StatefulSetReady, metav1.ConditionFalse, v1alpha1.ErrorCreatingStatefulSet,
			fmt.Sprintf("error creating or updating statefulSet for function: %v", err))
		return err
	}
	function.SetCondition(v1alpha1.StatefulSetReady, metav1.ConditionFalse, v1alpha1.PendingCreation,
		"creating or updating statefulSet for function...")
	return nil
}

func (r *FunctionReconciler) ObserveFunctionService(ctx context.Context, function *v1alpha1.Function) error {
	svc := &corev1.Service{}
	svcName := spec.MakeHeadlessServiceName(spec.MakeFunctionObjectMeta(function).Name)
	err := r.Get(ctx, types.NamespacedName{Namespace: function.Namespace,
		Name: svcName}, svc)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("function service is not created...",
				"namespace", function.Namespace, "name", function.Name,
				"service name", svcName)
			return nil
		}
		function.SetCondition(v1alpha1.Error, metav1.ConditionTrue, v1alpha1.ServiceError,
			fmt.Sprintf("error fetching function service: %v", err))
		return err
	}
	function.SetCondition(v1alpha1.ServiceReady, metav1.ConditionTrue, v1alpha1.ServiceIsReady, "")
	return nil
}

func (r *FunctionReconciler) ApplyFunctionService(ctx context.Context, function *v1alpha1.Function) error {
	if !r.checkIfServiceNeedUpdate(function) {
		return nil
	}
	desiredService := spec.MakeFunctionService(function)
	desiredServiceSpec := desiredService.Spec
	if _, err := ctrl.CreateOrUpdate(ctx, r.Client, desiredService, func() error {
		// function service mutate logic
		desiredService.Spec = desiredServiceSpec
		return nil
	}); err != nil {
		r.Log.Error(err, "error creating or updating service for function",
			"namespace", function.Namespace, "name", function.Name,
			"service name", desiredService.Name)
		function.SetCondition(v1alpha1.ServiceReady, metav1.ConditionFalse, v1alpha1.ErrorCreatingService,
			fmt.Sprintf("error creating or updating service for function: %v", err))
		return err
	}
	function.SetCondition(v1alpha1.ServiceReady, metav1.ConditionTrue, v1alpha1.ServiceIsReady, "")
	return nil
}

func (r *FunctionReconciler) ObserveFunctionHPA(ctx context.Context, function *v1alpha1.Function) error {
	if function.Spec.MaxReplicas == nil {
		// HPA not enabled, skip further action
		function.RemoveCondition(v1alpha1.HPAReady)
		return nil
	}

	hpa := &autov2beta2.HorizontalPodAutoscaler{}
	err := r.Get(ctx, types.NamespacedName{
		Namespace: function.Namespace,
		Name:      spec.MakeFunctionObjectMeta(function).Name}, hpa)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("hpa is not created for function...",
				"namespace", function.Namespace, "name", function.Name,
				"hpa name", hpa.Name)
			return nil
		}
		function.SetCondition(v1alpha1.Error, metav1.ConditionTrue, v1alpha1.HPAError,
			fmt.Sprintf("error fetching function hpa: %v", err))
		return err
	}

	function.SetCondition(v1alpha1.HPAReady, metav1.ConditionTrue, v1alpha1.HPAIsReady, "")
	return nil
}

func (r *FunctionReconciler) ApplyFunctionHPA(ctx context.Context, function *v1alpha1.Function) error {
	if function.Spec.MaxReplicas == nil {
		// HPA not enabled, clear the exists HPA
		hpa := &autov2beta2.HorizontalPodAutoscaler{}
		hpa.Namespace = function.Namespace
		hpa.Name = spec.MakeFunctionObjectMeta(function).Name
		if err := r.Delete(ctx, hpa); err != nil {
			if errors.IsNotFound(err) {
				return nil
			}
			function.SetCondition(v1alpha1.Error, metav1.ConditionTrue, v1alpha1.HPAError,
				fmt.Sprintf("error deleting hpa for function: %v", err))
			return err
		}
		return nil
	}

	if !r.checkIfHPANeedUpdate(function) {
		return nil
	}
	desiredHPA := spec.MakeFunctionHPA(function)
	desiredHPASpec := desiredHPA.Spec
	if _, err := ctrl.CreateOrUpdate(ctx, r.Client, desiredHPA, func() error {
		// function hpa mutate logic
		desiredHPA.Spec = desiredHPASpec
		return nil
	}); err != nil {
		r.Log.Error(err, "error creating or updating hpa for function",
			"namespace", function.Namespace, "name", function.Name,
			"hpa name", desiredHPA.Name)
		function.SetCondition(v1alpha1.HPAReady, metav1.ConditionFalse, v1alpha1.ErrorCreatingHPA,
			fmt.Sprintf("error creating or updating hpa for function: %v", err))
		return err
	}
	function.SetCondition(v1alpha1.HPAReady, metav1.ConditionTrue, v1alpha1.HPAIsReady, "")
	return nil
}

func (r *FunctionReconciler) ObserveFunctionVPA(ctx context.Context, function *v1alpha1.Function) error {
	if function.Spec.Pod.VPA == nil {
		// VPA not enabled, skip further action
		function.RemoveCondition(v1alpha1.VPAReady)
		return nil
	}

	vpa := &vpav1.VerticalPodAutoscaler{}
	err := r.Get(ctx, types.NamespacedName{
		Namespace: function.Namespace,
		Name:      spec.MakeFunctionObjectMeta(function).Name}, vpa)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("vpa is not created for function...",
				"namespace", function.Namespace, "name", function.Name,
				"vpa name", vpa.Name)
			return nil
		}
		function.SetCondition(v1alpha1.Error, metav1.ConditionTrue, v1alpha1.VPAError,
			fmt.Sprintf("error fetching function vpa: %v", err))
		return err
	}

	function.SetCondition(v1alpha1.VPAReady, metav1.ConditionTrue, v1alpha1.VPAIsReady, "")
	return nil
}

func (r *FunctionReconciler) ApplyFunctionVPA(ctx context.Context, function *v1alpha1.Function) error {
	if function.Spec.Pod.VPA == nil {
		// VPA not enabled, clear the exists VPA
		vpa := &vpav1.VerticalPodAutoscaler{}
		vpa.Namespace = function.Namespace
		vpa.Name = spec.MakeFunctionObjectMeta(function).Name
		if err := r.Delete(ctx, vpa); err != nil {
			if errors.IsNotFound(err) {
				return nil
			}
			function.SetCondition(v1alpha1.Error, metav1.ConditionTrue, v1alpha1.VPAError,
				fmt.Sprintf("error deleting vpa for function: %v", err))
			return err
		}
		return nil
	}

	if !r.checkIfVPANeedUpdate(function) {
		return nil
	}
	desiredVPA := spec.MakeFunctionVPA(function)
	desiredVPASpec := desiredVPA.Spec
	if _, err := ctrl.CreateOrUpdate(ctx, r.Client, desiredVPA, func() error {
		// function vpa mutate logic
		desiredVPA.Spec = desiredVPASpec
		return nil
	}); err != nil {
		r.Log.Error(err, "error creating or updating vpa for function",
			"namespace", function.Namespace, "name", function.Name,
			"vpa name", desiredVPA.Name)
		function.SetCondition(v1alpha1.VPAReady, metav1.ConditionFalse, v1alpha1.ErrorCreatingVPA,
			fmt.Sprintf("error creating or updating vpa for function: %v", err))
		return err
	}
	function.SetCondition(v1alpha1.VPAReady, metav1.ConditionTrue, v1alpha1.VPAIsReady, "")
	return nil
}

func (r *FunctionReconciler) checkIfStatefulSetNeedUpdate(function *v1alpha1.Function) bool {
	if statefulSetStatus := meta.FindStatusCondition(function.Status.Conditions, string(v1alpha1.StatefulSetReady)); statefulSetStatus != nil {
		if statefulSetStatus.ObservedGeneration != function.Generation {
			return true
		}
		if statefulSetStatus.Status == metav1.ConditionTrue {
			return false
		}
	}
	return true
}

func (r *FunctionReconciler) checkIfServiceNeedUpdate(function *v1alpha1.Function) bool {
	if serviceStatus := meta.FindStatusCondition(function.Status.Conditions, string(v1alpha1.ServiceReady)); serviceStatus != nil {
		if serviceStatus.ObservedGeneration != function.Generation {
			return true
		}
		if serviceStatus.Status == metav1.ConditionTrue {
			return false
		}
	}
	return true
}

func (r *FunctionReconciler) checkIfHPANeedUpdate(function *v1alpha1.Function) bool {
	if hpaStatus := meta.FindStatusCondition(function.Status.Conditions, string(v1alpha1.HPAReady)); hpaStatus != nil {
		if hpaStatus.ObservedGeneration != function.Generation {
			return true
		}
		if hpaStatus.Status == metav1.ConditionTrue {
			return false
		}
	}
	return true
}

func (r *FunctionReconciler) checkIfVPANeedUpdate(function *v1alpha1.Function) bool {
	if vpaStatus := meta.FindStatusCondition(function.Status.Conditions, string(v1alpha1.VPAReady)); vpaStatus != nil {
		if vpaStatus.ObservedGeneration != function.Generation {
			return true
		}
		if vpaStatus.Status == metav1.ConditionTrue {
			return false
		}
	}
	return true
}
