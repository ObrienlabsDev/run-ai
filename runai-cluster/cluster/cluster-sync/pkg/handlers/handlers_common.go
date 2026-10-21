/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
 */

package handlers

import (
	"context"
	"sync"

	"go.uber.org/zap"

	"github.com/dsx-ai-factory/run-ai/runai-cluster/cluster/cluster-sync/pkg/util"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type UpdateNamespaceHandler func(namespace *corev1.Namespace)
type UpdateProjectHandler func(project *util.ProjectData)

func UpdateNamespaceHandlerWrapper(updateHandler UpdateNamespaceHandler, cachedReader client.Reader, objectsName string) {
	namespaces, err := util.GetAllRunAiManagedNamespaces(cachedReader)
	if err != nil {
		zap.S().Errorf("Failed handling update event for %s, error: %v", objectsName, err)
		return
	}

	for _, namespace := range namespaces {
		updateHandler(&namespace)
	}
}

func UpdateProjectHandlerWrapper(updateHandler UpdateProjectHandler, cachedReader client.Reader, objectsName string) {
	projects, err := util.GetAllProjects(cachedReader)
	if err != nil {
		zap.S().Errorf("Failed handling update event for %s, error: %v", objectsName, err)
		return
	}

	for _, project := range projects {
		updateHandler(&project)
	}
}

func WasSynced(objectUID types.UID, objectNamespace string,
	syncedObjects map[string]map[types.UID]bool, syncedObjectsMutex *sync.RWMutex) bool {
	syncedObjectsMutex.RLock()
	syncedInNamespace, found := syncedObjects[objectNamespace]
	syncedObjectsMutex.RUnlock()

	if !found || syncedInNamespace == nil {
		return false
	}

	_, found = syncedInNamespace[objectUID]
	return found
}

// deletedDuringUpdate reports whether the object under key was deleted, or replaced, while its queued update
// was being sent. The informer drops an object from the cache before calling the delete handler, which doesn't
// wait for queued updates, so the object's delete may have reached the backend before that update.
func deletedDuringUpdate[T client.Object](cachedReader client.Reader, key client.ObjectKey, current T,
	isSameObject func(T) bool) (bool, error) {
	err := cachedReader.Get(context.Background(), key, current)
	if apierrors.IsNotFound(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return !isSameObject(current), nil
}
