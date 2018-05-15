package rolloutsblocked

import (
	"strings"

	"github.com/golang/glog"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	shipperlisters "github.com/bookingcom/shipper/pkg/client/listers/shipper/v1"
)

// GetBlocks returns a list RolloutBlock objects from the the globalNs and
// localNs namespaces. Returns a nil slice on error.
func Blocks(lister shipperlisters.RolloutBlockLister, globalNs, localNs string) []*shipperv1.RolloutBlock {
	everything := labels.Everything()

	allBlocks, err := lister.RolloutBlocks(globalNs).List(everything)
	if err != nil {
		glog.Error(err)
		return nil
	}

	localBlocks, err := lister.RolloutBlocks(localNs).List(everything)
	if err != nil {
		glog.Error(err)
		return nil
	}
	allBlocks = append(allBlocks, localBlocks...)

	return allBlocks
}

// Check returns true when rollouts are blocked and false otherwise.
// If app or rel are set, it also checks for overrides. If both app and rel are
// set, rollouts are considered not blocked if either one has the full set of
// correct overrides. If neither is set, the block is computed simply from
// presence of RolloutBlock objects.
func Check(blocks []*shipperv1.RolloutBlock, app *shipperv1.Application, rel *shipperv1.Release) bool {
	if app == nil && rel == nil {
		return len(blocks) > 0
	}
	if app != nil && overridesBlocks(blocks, app) {
		return false
	}
	if rel != nil && overridesBlocks(blocks, rel) {
		return false
	}

	return true
}

func overridesBlocks(blocks []*shipperv1.RolloutBlock, obj metav1.Object) bool {
	overrides := getOverrides(obj)

	left := len(blocks)
	for _, b := range blocks {
		key := b.Namespace + "/" + b.Name
		for _, override := range overrides {
			if override == key {
				left--
				break
			}
		}
	}

	return left == 0
}

func getOverrides(obj metav1.Object) []string {
	return strings.Split(obj.GetAnnotations()[shipperv1.RolloutBlockOverrideAnnotation], ",")
}
