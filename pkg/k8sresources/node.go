package k8sresources

import (
	"fmt"
	"strings"

	"github.com/bonnefoa/kubectl-fzf/pkg/util"
	corev1 "k8s.io/api/core/v1"
)

const NodeHeader = "Name Roles InstanceType Zone InternalIp Age Labels\n"

// Node is the summary of a kubernetes node
type Node struct {
	ResourceMeta
	roles        []string
	instanceType string
	zone         string
	internalIP   string
}

// NewNodeFromRuntime builds a k8sresoutce from informer result
func NewNodeFromRuntime(obj interface{}) K8sResource {
	n := &Node{}
	n.FromRuntime(obj)
	return n
}

// FromRuntime builds object from the informer's result
func (n *Node) FromRuntime(obj interface{}) {
	node := obj.(*corev1.Node)
	n.FromObjectMeta(node.ObjectMeta)
	for k, _ := range n.labels {
		nodePrefix := "node-role.kubernetes.io/"
		if strings.HasPrefix(k, nodePrefix) {
			role := strings.Replace(k, nodePrefix, "", 1)
			n.roles = append(n.roles, role)
		}
	}
	n.instanceType = n.labels["beta.kubernetes.io/instance-type"]
	n.zone = n.labels["failure-domain.beta.kubernetes.io/zone"]
	for _, v := range node.Status.Addresses {
		if v.Type == "InternalIP" {
			n.internalIP = v.Address
		}
	}
}

// HasChanged returns true if the resource's dump needs to be updated
func (n *Node) HasChanged(k K8sResource) bool {
	return true
}

// ToString serializes the object to strings
func (n *Node) ToString() string {
	line := strings.Join([]string{n.name,
		util.JoinSlicesOrNone(n.roles, ","),
		n.instanceType,
		n.zone,
		n.internalIP,
		n.resourceAge(),
		n.labelsString(),
	}, " ")
	return fmt.Sprintf("%s\n", line)
}
