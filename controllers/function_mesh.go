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
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	computeapi "github.com/streamnative/function-mesh/api/compute/v1alpha2"
	"github.com/streamnative/function-mesh/controllers/spec"
	apispec "github.com/streamnative/function-mesh/pkg/spec"
)

func (r *FunctionMeshReconciler) ObserveFunctionMesh(ctx context.Context, mesh *computeapi.FunctionMesh) error {
	if err := r.observeFunctions(ctx, mesh); err != nil {
		return err
	}
	if err := r.observeSources(ctx, mesh); err != nil {
		return err
	}
	if err := r.observeSinks(ctx, mesh); err != nil {
		return err
	}
	// Observation only
	r.observeMeshes(mesh)
	return nil
}

func (r *FunctionMeshReconciler) observeFunctions(ctx context.Context, mesh *computeapi.FunctionMesh) error {
	orphanedFunctions := map[string]bool{}
	unreadyFunctions := []string{}

	defer func() {
		if len(mesh.Spec.Functions) > 0 {
			if len(unreadyFunctions) == 0 {
				mesh.SetCondition(apispec.FunctionReady, metav1.ConditionTrue, apispec.FunctionIsReady, "")
				return
			}
			mesh.SetCondition(apispec.FunctionReady, metav1.ConditionFalse, apispec.PendingCreation,
				"wait for sub functions to be ready")
		} else {
			mesh.RemoveCondition(apispec.FunctionReady)
		}
	}()

	if len(mesh.Status.FunctionConditions) > 0 {
		for name := range mesh.Status.FunctionConditions {
			orphanedFunctions[name] = true
		}
	}

	for _, functionSpec := range mesh.Spec.Functions {
		delete(orphanedFunctions, functionSpec.Name)

		// present the original name to use in Status, but underlying use the complete-name
		function := &computeapi.Function{}
		err := r.Get(ctx, types.NamespacedName{
			Namespace: mesh.Namespace,
			Name:      makeComponentName(mesh.Name, functionSpec.Name),
		}, function)
		if err != nil {
			if errors.IsNotFound(err) {
				r.Log.Info("mesh function is not ready yet...",
					"namespace", mesh.Namespace, "name", mesh.Name,
					"function name", functionSpec.Name)
				mesh.Status.FunctionConditions[functionSpec.Name].SetStatus(apispec.Wait)
				mesh.SetCondition(apispec.FunctionReady, metav1.ConditionFalse, apispec.PendingCreation,
					"mesh function is not ready yet...")
				unreadyFunctions = append(unreadyFunctions, functionSpec.Name)
				continue
			}
			mesh.Status.FunctionConditions[functionSpec.Name].SetStatus(apispec.Error)
			mesh.SetCondition(apispec.Error, metav1.ConditionTrue, apispec.FunctionError,
				fmt.Sprintf("error fetching function: %v", err))
			unreadyFunctions = append(unreadyFunctions, functionSpec.Name)
			return err
		}

		// if the function needs to be updated or if the function is not ready,
		// it will be added to the unready inventory.
		if r.checkIfFunctionNeedUpdate(mesh, &functionSpec) ||
			!meta.IsStatusConditionTrue(function.Status.Conditions, string(apispec.Ready)) {
			mesh.Status.FunctionConditions[functionSpec.Name].SetStatus(apispec.Wait)
			unreadyFunctions = append(unreadyFunctions, functionSpec.Name)
			continue
		}
		mesh.Status.FunctionConditions[functionSpec.Name].SetStatus(apispec.Ready)
	}

	for name, isOrphaned := range orphanedFunctions {
		if isOrphaned {
			mesh.Status.FunctionConditions[name].SetStatus(apispec.Orphaned)
		}
	}
	return nil
}

func (r *FunctionMeshReconciler) observeSources(ctx context.Context, mesh *computeapi.FunctionMesh) error {
	orphanedSources := map[string]bool{}
	unreadySources := []string{}

	defer func() {
		if len(mesh.Spec.Sources) > 0 {
			if len(unreadySources) == 0 {
				mesh.SetCondition(apispec.SourceReady, metav1.ConditionTrue, apispec.SourceIsReady, "")
				return
			}
			mesh.SetCondition(apispec.SourceReady, metav1.ConditionFalse, apispec.PendingCreation,
				"wait for sub sources to be ready")
		} else {
			mesh.RemoveCondition(apispec.SourceReady)
		}
	}()

	if len(mesh.Status.SourceConditions) > 0 {
		for name := range mesh.Status.SourceConditions {
			orphanedSources[name] = true
		}
	}

	for _, sourceSpec := range mesh.Spec.Sources {
		delete(orphanedSources, sourceSpec.Name)

		// present the original name to use in Status, but underlying use the complete-name
		source := &computeapi.Source{}
		err := r.Get(ctx, types.NamespacedName{
			Namespace: mesh.Namespace,
			Name:      makeComponentName(mesh.Name, sourceSpec.Name),
		}, source)
		if err != nil {
			if errors.IsNotFound(err) {
				r.Log.Info("mesh source is not ready yet...",
					"namespace", mesh.Namespace, "name", mesh.Name,
					"source name", sourceSpec.Name)
				mesh.Status.SourceConditions[sourceSpec.Name].SetStatus(apispec.Wait)
				mesh.SetCondition(apispec.SourceReady, metav1.ConditionFalse, apispec.PendingCreation,
					"mesh source is not ready yet...")
				unreadySources = append(unreadySources, sourceSpec.Name)
				continue
			}
			mesh.Status.SourceConditions[sourceSpec.Name].SetStatus(apispec.Error)
			mesh.SetCondition(apispec.Error, metav1.ConditionTrue, apispec.SourceError,
				fmt.Sprintf("error fetching source: %v", err))
			unreadySources = append(unreadySources, sourceSpec.Name)
			return err
		}

		// if the source needs to be updated or if the source is not ready,
		// it will be added to the unready inventory.
		if r.checkIfSourceNeedUpdate(mesh, &sourceSpec) ||
			!meta.IsStatusConditionTrue(source.Status.Conditions, string(apispec.Ready)) {
			mesh.Status.SourceConditions[sourceSpec.Name].SetStatus(apispec.Wait)
			unreadySources = append(unreadySources, sourceSpec.Name)
			continue
		}
		mesh.Status.SourceConditions[sourceSpec.Name].SetStatus(apispec.Ready)
	}

	for name, isOrphaned := range orphanedSources {
		if isOrphaned {
			mesh.Status.SourceConditions[name].SetStatus(apispec.Orphaned)
		}
	}
	return nil
}

func (r *FunctionMeshReconciler) observeSinks(ctx context.Context, mesh *computeapi.FunctionMesh) error {
	orphanedSinks := map[string]bool{}
	unreadySinks := []string{}

	defer func() {
		if len(mesh.Spec.Sinks) > 0 {
			if len(unreadySinks) == 0 {
				mesh.SetCondition(apispec.SinkReady, metav1.ConditionTrue, apispec.SinkIsReady, "")
				return
			}
			mesh.SetCondition(apispec.SinkReady, metav1.ConditionFalse, apispec.PendingCreation,
				"wait for sub sinks to be ready")
		} else {
			mesh.RemoveCondition(apispec.SinkReady)
		}
	}()

	if len(mesh.Status.SinkConditions) > 0 {
		for name := range mesh.Status.SinkConditions {
			orphanedSinks[name] = true
		}
	}

	for _, sinkSpec := range mesh.Spec.Sinks {
		delete(orphanedSinks, sinkSpec.Name)

		// present the original name to use in Status, but underlying use the complete-name
		sink := &computeapi.Sink{}
		err := r.Get(ctx, types.NamespacedName{
			Namespace: mesh.Namespace,
			Name:      makeComponentName(mesh.Name, sinkSpec.Name),
		}, sink)
		if err != nil {
			if errors.IsNotFound(err) {
				r.Log.Info("mesh sink is not ready yet...",
					"namespace", mesh.Namespace, "name", mesh.Name,
					"sink name", sinkSpec.Name)
				mesh.Status.SinkConditions[sinkSpec.Name].SetStatus(apispec.Wait)
				mesh.SetCondition(apispec.SinkReady, metav1.ConditionFalse, apispec.PendingCreation,
					"mesh sink is not ready yet...")
				unreadySinks = append(unreadySinks, sinkSpec.Name)
				continue
			}
			mesh.Status.SinkConditions[sinkSpec.Name].SetStatus(apispec.Error)
			mesh.SetCondition(apispec.Error, metav1.ConditionTrue, apispec.SinkError,
				fmt.Sprintf("error fetching sink: %v", err))
			unreadySinks = append(unreadySinks, sinkSpec.Name)
			return err
		}

		// if the sink needs to be updated or if the sink is not ready,
		// it will be added to the unready inventory.
		if r.checkIfSinkNeedUpdate(mesh, &sinkSpec) ||
			!meta.IsStatusConditionTrue(sink.Status.Conditions, string(apispec.Ready)) {
			mesh.Status.SinkConditions[sinkSpec.Name].SetStatus(apispec.Wait)
			unreadySinks = append(unreadySinks, sinkSpec.Name)
			continue
		}
		mesh.Status.SinkConditions[sinkSpec.Name].SetStatus(apispec.Ready)
	}

	for name, isOrphaned := range orphanedSinks {
		if isOrphaned {
			mesh.Status.SinkConditions[name].SetStatus(apispec.Orphaned)
		}
	}
	return nil
}

func (r *FunctionMeshReconciler) observeMeshes(mesh *computeapi.FunctionMesh) {
	functionReady := len(mesh.Spec.Functions) == 0 ||
		(len(mesh.Spec.Functions) > 0 && meta.IsStatusConditionTrue(mesh.Status.Conditions, string(apispec.FunctionReady)))
	sourceReady := len(mesh.Spec.Sources) == 0 ||
		(len(mesh.Spec.Sources) > 0 && meta.IsStatusConditionTrue(mesh.Status.Conditions, string(apispec.SourceReady)))
	sinkReady := len(mesh.Spec.Sinks) == 0 ||
		(len(mesh.Spec.Sinks) > 0 && meta.IsStatusConditionTrue(mesh.Status.Conditions, string(apispec.SinkReady)))

	if functionReady && sourceReady && sinkReady {
		mesh.SetCondition(apispec.Ready, metav1.ConditionTrue, apispec.MeshIsReady, "")
	} else {
		mesh.SetCondition(apispec.Ready, metav1.ConditionFalse, apispec.PendingCreation,
			"wait for sub components to be ready")
	}
}

func (r *FunctionMeshReconciler) UpdateFunctionMesh(ctx context.Context, mesh *computeapi.FunctionMesh) error {
	if meta.IsStatusConditionTrue(mesh.Status.Conditions, string(apispec.Ready)) {
		return nil
	}

	for _, functionSpec := range mesh.Spec.Functions {
		condition := mesh.Status.FunctionConditions[functionSpec.Name]
		if condition.Status == apispec.Ready {
			continue
		}
		desiredFunction := spec.MakeFunctionComponent(makeComponentName(mesh.Name, functionSpec.Name), mesh, &functionSpec)
		desiredFunctionSpec := desiredFunction.Spec
		if _, err := ctrl.CreateOrUpdate(ctx, r.Client, desiredFunction, func() error {
			// function mutate logic
			desiredFunction.Spec = desiredFunctionSpec
			return nil
		}); err != nil {
			r.Log.Error(err, "error creating or updating function",
				"namespace", mesh.Namespace, "name", mesh.Name,
				"function name", functionSpec.Name)
			mesh.Status.FunctionConditions[functionSpec.Name].SetStatus(apispec.Error)
			mesh.SetCondition(apispec.Error, metav1.ConditionTrue, apispec.ErrorCreatingFunction,
				fmt.Sprintf("error creating or updating function: %v", err))
			return err
		}
		mesh.Status.FunctionConditions[functionSpec.Name].SetStatus(apispec.Wait)
	}

	for _, sourceSpec := range mesh.Spec.Sources {
		condition := mesh.Status.SourceConditions[sourceSpec.Name]
		if condition.Status == apispec.Ready {
			continue
		}
		desiredSource := spec.MakeSourceComponent(makeComponentName(mesh.Name, sourceSpec.Name), mesh, &sourceSpec)
		desiredSourceSpec := desiredSource.Spec
		if _, err := ctrl.CreateOrUpdate(ctx, r.Client, desiredSource, func() error {
			// source mutate logic
			desiredSource.Spec = desiredSourceSpec
			return nil
		}); err != nil {
			r.Log.Error(err, "error creating or updating source",
				"namespace", mesh.Namespace, "name", mesh.Name,
				"source name", sourceSpec.Name)
			mesh.Status.SourceConditions[sourceSpec.Name].SetStatus(apispec.Error)
			mesh.SetCondition(apispec.Error, metav1.ConditionTrue, apispec.ErrorCreatingSource,
				fmt.Sprintf("error creating or updating source: %v", err))
			return err
		}
		mesh.Status.SourceConditions[sourceSpec.Name].SetStatus(apispec.Wait)
	}

	for _, sinkSpec := range mesh.Spec.Sinks {
		condition := mesh.Status.SinkConditions[sinkSpec.Name]
		if condition.Status == apispec.Ready {
			continue
		}
		desiredSink := spec.MakeSinkComponent(makeComponentName(mesh.Name, sinkSpec.Name), mesh, &sinkSpec)
		desiredSinkSpec := desiredSink.Spec
		if _, err := ctrl.CreateOrUpdate(ctx, r.Client, desiredSink, func() error {
			// sink mutate logic
			desiredSink.Spec = desiredSinkSpec
			return nil
		}); err != nil {
			r.Log.Error(err, "error creating or updating sink",
				"namespace", mesh.Namespace, "name", mesh.Name,
				"sink name", sinkSpec.Name)
			mesh.Status.SinkConditions[sinkSpec.Name].SetStatus(apispec.Error)
			mesh.SetCondition(apispec.Error, metav1.ConditionTrue, apispec.ErrorCreatingSink,
				fmt.Sprintf("error creating or updating sink: %v", err))
			return err
		}
		mesh.Status.SinkConditions[sinkSpec.Name].SetStatus(apispec.Wait)
	}

	// handle logic for cleaning up orphaned subcomponents
	if len(mesh.Spec.Functions) != len(mesh.Status.FunctionConditions) {
		for name, cond := range mesh.Status.FunctionConditions {
			if cond.Status == apispec.Orphaned {
				// clean up the orphaned functions
				function := &computeapi.Function{}
				function.Namespace = mesh.Namespace
				function.Name = makeComponentName(mesh.Name, name)
				if err := r.Delete(ctx, function); err != nil && !errors.IsNotFound(err) {
					r.Log.Error(err, "error deleting orphaned function for mesh",
						"namespace", mesh.Namespace, "name", mesh.Name,
						"function name", name)
					mesh.SetCondition(apispec.Error, metav1.ConditionTrue, apispec.FunctionError,
						fmt.Sprintf("error deleting orphaned function for mesh: %v", err))
					return err
				}
				delete(mesh.Status.FunctionConditions, name)
			}
		}
	}

	if len(mesh.Spec.Sources) != len(mesh.Status.SourceConditions) {
		for name, cond := range mesh.Status.SourceConditions {
			if cond.Status == apispec.Orphaned {
				// clean up the orphaned sources
				source := &computeapi.Source{}
				source.Namespace = mesh.Namespace
				source.Name = makeComponentName(mesh.Name, name)
				if err := r.Delete(ctx, source); err != nil && !errors.IsNotFound(err) {
					r.Log.Error(err, "error deleting orphaned source for mesh",
						"namespace", mesh.Namespace, "name", mesh.Name,
						"source name", name)
					mesh.SetCondition(apispec.Error, metav1.ConditionTrue, apispec.SourceError,
						fmt.Sprintf("error deleting orphaned source for mesh: %v", err))
					return err
				}
				delete(mesh.Status.SourceConditions, name)
			}
		}
	}

	if len(mesh.Spec.Sinks) != len(mesh.Status.SinkConditions) {
		for name, cond := range mesh.Status.SinkConditions {
			if cond.Status == apispec.Orphaned {
				// clean up the orphaned sinks
				sink := &computeapi.Sink{}
				sink.Namespace = mesh.Namespace
				sink.Name = makeComponentName(mesh.Name, name)
				if err := r.Delete(ctx, sink); err != nil && !errors.IsNotFound(err) {
					r.Log.Error(err, "error deleting orphaned sink for mesh",
						"namespace", mesh.Namespace, "name", mesh.Name,
						"sink name", name)
					mesh.SetCondition(apispec.Error, metav1.ConditionTrue, apispec.SinkError,
						fmt.Sprintf("error deleting orphaned sink for mesh: %v", err))
					return err
				}
				delete(mesh.Status.SinkConditions, name)
			}
		}
	}
	return nil
}

func makeComponentName(prefix, name string) string {
	return prefix + "-" + name
}

func (r *FunctionMeshReconciler) initializeMesh(mesh *computeapi.FunctionMesh) {
	// initialize function conditions
	if len(mesh.Spec.Functions) > 0 {
		if mesh.Status.FunctionConditions == nil {
			mesh.Status.FunctionConditions = make(map[string]*computeapi.ComponentCondition)
		}
		for _, function := range mesh.Spec.Functions {
			if _, exist := mesh.Status.FunctionConditions[function.Name]; !exist {
				specBytes, _ := json.Marshal(function)
				specHash := spec.GenerateSpecHash(specBytes)
				mesh.Status.FunctionConditions[function.Name] = &computeapi.ComponentCondition{
					Status: apispec.Wait,
					Hash:   &specHash,
				}
			}
		}
	} else {
		mesh.Status.FunctionConditions = nil
	}

	// initialize sink conditions
	if len(mesh.Spec.Sinks) > 0 {
		if mesh.Status.SinkConditions == nil {
			mesh.Status.SinkConditions = make(map[string]*computeapi.ComponentCondition)
		}
		for _, sink := range mesh.Spec.Sinks {
			if _, exist := mesh.Status.SinkConditions[sink.Name]; !exist {
				specBytes, _ := json.Marshal(sink)
				specHash := spec.GenerateSpecHash(specBytes)
				mesh.Status.SinkConditions[sink.Name] = &computeapi.ComponentCondition{
					Status: apispec.Wait,
					Hash:   &specHash,
				}
			}
		}
	} else {
		mesh.Status.SinkConditions = nil
	}

	// initialize source conditions
	if len(mesh.Spec.Sources) > 0 {
		if mesh.Status.SourceConditions == nil {
			mesh.Status.SourceConditions = make(map[string]*computeapi.ComponentCondition)
		}
		for _, source := range mesh.Spec.Sources {
			if _, exist := mesh.Status.SourceConditions[source.Name]; !exist {
				specBytes, _ := json.Marshal(source)
				specHash := spec.GenerateSpecHash(specBytes)
				mesh.Status.SourceConditions[source.Name] = &computeapi.ComponentCondition{
					Status: apispec.Wait,
					Hash:   &specHash,
				}
			}
		}
	} else {
		mesh.Status.SourceConditions = nil
	}
}

func (r *FunctionMeshReconciler) checkIfFunctionNeedUpdate(mesh *computeapi.FunctionMesh, functionSpec *computeapi.FunctionSpec) bool {
	desiredObject := spec.MakeFunctionComponent(makeComponentName(mesh.Name, functionSpec.Name), mesh, functionSpec)
	desiredSpecBytes, _ := json.Marshal(desiredObject.Spec)
	cond, exist := mesh.Status.FunctionConditions[functionSpec.Name]
	if exist {
		if specHash := cond.Hash; specHash != nil {
			// if the desired specification has not changed, we do not need to update the component
			if *specHash == spec.GenerateSpecHash(desiredSpecBytes) {
				return false
			}
		}
	}
	return true
}

func (r *FunctionMeshReconciler) checkIfSourceNeedUpdate(mesh *computeapi.FunctionMesh, sourceSpec *computeapi.SourceSpec) bool {
	desiredObject := spec.MakeSourceComponent(makeComponentName(mesh.Name, sourceSpec.Name), mesh, sourceSpec)
	desiredSpecBytes, _ := json.Marshal(desiredObject.Spec)
	cond, exist := mesh.Status.SourceConditions[sourceSpec.Name]
	if exist {
		if specHash := cond.Hash; specHash != nil {
			// if the desired specification has not changed, we do not need to update the component
			if *specHash == spec.GenerateSpecHash(desiredSpecBytes) {
				return false
			}
		}
	}
	return true
}

func (r *FunctionMeshReconciler) checkIfSinkNeedUpdate(mesh *computeapi.FunctionMesh, sinkSpec *computeapi.SinkSpec) bool {
	desiredObject := spec.MakeSinkComponent(makeComponentName(mesh.Name, sinkSpec.Name), mesh, sinkSpec)
	desiredSpecBytes, _ := json.Marshal(desiredObject.Spec)
	cond, exist := mesh.Status.SinkConditions[sinkSpec.Name]
	if exist {
		if specHash := cond.Hash; specHash != nil {
			// if the desired specification has not changed, we do not need to update the component
			if *specHash == spec.GenerateSpecHash(desiredSpecBytes) {
				return false
			}
		}
	}
	return true
}
