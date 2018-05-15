package rolloutsblocked

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
)

const ns = "application-namespace"

var (
	overrides = []string{
		shipperv1.ShipperNamespace + "/" + "prod-is-on-fire",
		ns + "/" + "capacity-test-in-progress",
	}

	blocks = []*shipperv1.RolloutBlock{
		&shipperv1.RolloutBlock{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "prod-is-on-fire",
				Namespace: shipperv1.ShipperNamespace,
			},
			Spec: shipperv1.RolloutBlockSpec{
				Message: "prod is on fire",
				Author:  shipperv1.RolloutBlockAuthor{Name: "foo"},
			},
		},
		&shipperv1.RolloutBlock{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "capacity-test-in-progress",
				Namespace: ns,
			},
			Spec: shipperv1.RolloutBlockSpec{
				Message: "capacity test is in progress",
				Author:  shipperv1.RolloutBlockAuthor{Name: "bar"},
			},
		},
	}

	appWithOverride = &shipperv1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "app-with-override",
			Namespace: ns,
			Annotations: map[string]string{
				shipperv1.RolloutBlockOverrideAnnotation: strings.Join(overrides, ","),
			},
		},
	}
	appNoOverride = &shipperv1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "app-without-override",
			Namespace: ns,
		},
	}
	appEmptyOverride = &shipperv1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "app-with-empty-override",
			Namespace:   ns,
			Annotations: map[string]string{shipperv1.RolloutBlockOverrideAnnotation: ""},
		},
	}
	appMismatchedOverride = &shipperv1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "app-with-empty-override",
			Namespace:   ns,
			Annotations: map[string]string{shipperv1.RolloutBlockOverrideAnnotation: "foobar"},
		},
	}

	relWithOverride = &shipperv1.Release{
		ReleaseMeta: shipperv1.ReleaseMeta{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "release-with-override",
				Namespace:   ns,
				Annotations: map[string]string{shipperv1.RolloutBlockOverrideAnnotation: strings.Join(overrides, ",")},
			},
		},
	}
	relNoOverride = &shipperv1.Release{
		ReleaseMeta: shipperv1.ReleaseMeta{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "release-without-override",
				Namespace: ns,
			},
		},
	}
	relEmptyOverride = &shipperv1.Release{
		ReleaseMeta: shipperv1.ReleaseMeta{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "release-with-empty-override",
				Namespace:   ns,
				Annotations: map[string]string{shipperv1.RolloutBlockOverrideAnnotation: ""},
			},
		},
	}
	relMismatchedOverride = &shipperv1.Release{
		ReleaseMeta: shipperv1.ReleaseMeta{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "release-with-empty-override",
				Namespace:   ns,
				Annotations: map[string]string{shipperv1.RolloutBlockOverrideAnnotation: "foobar"},
			},
		},
	}
)

func TestCheckNoBlocks(t *testing.T) {
	if Check(nil, nil, nil) {
		t.Error("no blocks: expected rollouts to not be blocked")
	}
}

func TestCheckNoBlocksWithOverrides(t *testing.T) {
	if Check(nil, appWithOverride, nil) {
		t.Error("no blocks, app override: expected rollouts to not be blocked")
	}
	if Check(nil, nil, relWithOverride) {
		t.Error("no blocks, rel override: expected rollouts to not be blocked")
	}
}

func TestCheckBlocksWithoutOverrides(t *testing.T) {
	if !Check(blocks, nil, nil) {
		t.Error("blocks, no overrides: expected rollouts to be blocked")
	}
	if !Check(blocks, appEmptyOverride, nil) {
		t.Error("blocks, empty app override: expected rollouts to be blocked")
	}
	if !Check(blocks, nil, relEmptyOverride) {
		t.Error("blocks, empty rel override: expected rollouts to be blocked")
	}
	if !Check(blocks, appNoOverride, nil) {
		t.Error("blocks, no app override: expected rollouts to be blocked")
	}
	if !Check(blocks, nil, relNoOverride) {
		t.Error("blocks, no rel override: expected rollouts to be blocked")
	}
}

func TestCheckBlocksWithMismatchedOverrides(t *testing.T) {
	if !Check(blocks, appMismatchedOverride, nil) {
		t.Error("blocks, mismatched app override: expected rollouts to be blocked")
	}
	if !Check(blocks, nil, relMismatchedOverride) {
		t.Error("blocks, mismatched rel override: expected rollouts to be blocked")
	}
}

func TestCheckBlocksWithOverrides(t *testing.T) {
	if Check(blocks, appWithOverride, nil) {
		t.Error("blocks, app overide: expected rollouts to not be blocked")
	}
	if Check(blocks, nil, relWithOverride) {
		t.Error("blocks, rel overide: expected rollouts to not be blocked")
	}
	if Check(blocks, appWithOverride, relWithOverride) {
		t.Error("blocks, rel and app overides: expected rollouts to not be blocked")
	}
}
