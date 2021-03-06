package resourcewatcher

import (
	"bufio"
	"fmt"
	"os"
	"path"
	"time"

	"github.com/bonnefoa/kubectl-fzf/pkg/k8sresources"
	"github.com/golang/glog"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
)

// K8sStore stores the current state of k8s resources
type K8sStore struct {
	data         map[string]k8sresources.K8sResource
	resourceCtor func(obj interface{}) k8sresources.K8sResource
	header       string
	resourceName string
	destFile     string
	tempFileName string
	currentFile  *os.File
	lastFullDump time.Time
	storeConfig  StoreConfig
	firstWrite   bool
}

type StoreConfig struct {
	Cluster             string
	CacheDir            string
	TimeBetweenFullDump time.Duration
}

// NewK8sStore creates a new store
func NewK8sStore(cfg watchConfig, storeConfig StoreConfig) (K8sStore, error) {
	k := K8sStore{}
	destDir := path.Join(storeConfig.CacheDir, storeConfig.Cluster)
	destFile := path.Join(destDir, cfg.resourceName)
	err := os.MkdirAll(destDir, os.ModePerm)
	if err != nil {
		return k, errors.Wrapf(err, "Error creating directory %s", destDir)
	}
	k.tempFileName = fmt.Sprintf("%s_", destFile)
	currentFile, err := os.Create(k.tempFileName)
	if err != nil {
		return k, errors.Wrapf(err, "Error creating file %s", k.tempFileName)
	}
	k.data = make(map[string]k8sresources.K8sResource, 0)
	k.resourceCtor = cfg.resourceCtor
	k.resourceName = cfg.resourceName
	k.header = cfg.header
	k.destFile = destFile
	k.currentFile = currentFile
	k.lastFullDump = time.Time{}
	k.storeConfig = storeConfig
	k.firstWrite = true
	return k, nil
}

func resourceKey(obj interface{}) string {
	o := obj.(metav1.ObjectMetaAccessor).GetObjectMeta()
	return fmt.Sprintf("%s_%s", o.GetNamespace(), o.GetName())
}

// AddResourceList clears current state add the objects to the store.
// It will trigger a full dump
func (k *K8sStore) AddResourceList(lstRuntime []runtime.Object) {
	k.data = make(map[string]k8sresources.K8sResource, 0)
	for _, runtimeObject := range lstRuntime {
		key := resourceKey(runtimeObject)
		resource := k.resourceCtor(runtimeObject)
		k.data[key] = resource
	}
	err := k.DumpFullState()
	if err != nil {
		glog.Warningf("Error when dumping state: %v", err)
	}
}

// AddResource adds a new k8s object to the store
func (k *K8sStore) AddResource(obj interface{}) {
	key := resourceKey(obj)
	newObj := k.resourceCtor(obj)
	glog.V(11).Infof("%s added: %s", k.resourceName, key)
	k.data[key] = newObj

	err := k.AppendNewObject(newObj)
	if err != nil {
		glog.Warningf("Error when appending new object to current state: %v", err)
	}
}

// DeleteResource removes an existing k8s object to the store
func (k *K8sStore) DeleteResource(obj interface{}) {
	key := "Unknown"
	switch v := obj.(type) {
	case cache.DeletedFinalStateUnknown:
		key = resourceKey(v.Obj)
	case metav1.ObjectMetaAccessor:
		key = resourceKey(obj)
	default:
		glog.V(6).Infof("Unknown object type %v", obj)
	}
	glog.V(11).Infof("%s deleted: %s", k.resourceName, key)
	delete(k.data, key)

	err := k.DumpFullState()
	if err != nil {
		glog.Warningf("Error when dumping state: %v", err)
	}
}

// UpdateResource update an existing k8s object
func (k *K8sStore) UpdateResource(oldObj, newObj interface{}) {
	key := resourceKey(newObj)
	k8sObj := k.resourceCtor(newObj)
	if k8sObj.HasChanged(k.data[key]) {
		glog.V(11).Infof("%s changed: %s", k.resourceName, key)
		k.data[key] = k8sObj
		err := k.DumpFullState()
		if err != nil {
			glog.Warningf("Error when dumping state: %v", err)
		}
	}
}

// AppendNewObject appends a new object to the cache dump
func (k *K8sStore) AppendNewObject(resource k8sresources.K8sResource) error {
	if k.firstWrite {
		k.currentFile.WriteString(k.header)
		k.firstWrite = false
		err := os.Rename(k.tempFileName, k.destFile)
		if err != nil {
			return err
		}
	}
	_, err := k.currentFile.WriteString(resource.ToString())
	if err != nil {
		return err
	}
	return nil
}

// DumpFullState writes the full state to the cache file
func (k *K8sStore) DumpFullState() error {
	now := time.Now()
	delta := now.Sub(k.lastFullDump)
	if delta < k.storeConfig.TimeBetweenFullDump {
		glog.V(10).Infof("Last full dump for %s happened %s ago, ignoring it", k.resourceName, delta)
		return nil
	}
	k.lastFullDump = now
	glog.V(8).Infof("Doing full dump %d %s", len(k.data), k.resourceName)
	tempFileName, err := os.Create(k.tempFileName)
	glog.V(12).Infof("Creating temp file for full state %s", tempFileName.Name())
	if err != nil {
		return errors.Wrapf(err, "Error creating temp file %s", k.tempFileName)
	}
	w := bufio.NewWriter(tempFileName)
	w.WriteString(k.header)
	for _, v := range k.data {
		_, err := w.WriteString(v.ToString())
		if err != nil {
			return errors.Wrapf(err, "Error writing bytes to file %s", k.tempFileName)
		}
	}
	err = w.Flush()
	if err != nil {
		return errors.Wrapf(err, "Error flushing buffer")
	}

	err = tempFileName.Sync()
	if err != nil {
		return errors.Wrapf(err, "Error syncing file")
	}

	glog.V(17).Infof("Closing file %s", k.currentFile.Name())
	k.currentFile.Close()
	err = os.Rename(k.tempFileName, k.destFile)
	if err != nil {
		return errors.Wrapf(err, "Error moving file from %s to %s",
			k.tempFileName, k.destFile)
	}
	k.currentFile = tempFileName
	return nil
}
