package application

import (
	"fmt"
	"os"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubetesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"
	"k8s.io/helm/pkg/repo/repotest"

	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	shipperfake "github.com/bookingcom/shipper/pkg/client/clientset/versioned/fake"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
	shippertesting "github.com/bookingcom/shipper/pkg/testing"
	apputil "github.com/bookingcom/shipper/pkg/util/application"
	releaseutil "github.com/bookingcom/shipper/pkg/util/release"
)

const (
	testAppName = "test-app"
)

func init() {
	apputil.ConditionsShouldDiscardTimestamps = true
}

// Private method, but other tests make use of it.

func TestHashReleaseEnv(t *testing.T) {

	app := newApplication(testAppName)
	rel := newRelease("test-release", app)

	appHash := hashReleaseEnvironment(app.Spec.Template)
	relHash := hashReleaseEnvironment(rel.Spec.Environment)
	if appHash != relHash {
		t.Errorf("two identical environments should have hashed to the same value, but they did not: app %q and rel %q", appHash, relHash)
	}

	distinctApp := newApplication(testAppName)
	distinctApp.Spec.Template.Strategy = &shipper.RolloutStrategy{}
	distinctHash := hashReleaseEnvironment(distinctApp.Spec.Template)
	if distinctHash == appHash {
		t.Errorf("two different environments hashed to the same thing: %q", distinctHash)
	}
}

// An app with no history should create a release.

func TestCreateFirstRelease(t *testing.T) {
	f := newFixture(t)
	app := newApplication(testAppName)
	app.Spec.Template.Chart.RepoURL = "127.0.0.1"

	envHash := hashReleaseEnvironment(app.Spec.Template)
	expectedRelName := fmt.Sprintf("%s-%s-0", testAppName, envHash)

	f.objects = append(f.objects, app)
	expectedApp := app.DeepCopy()
	expectedApp.Annotations[shipper.AppHighestObservedGenerationAnnotation] = "0"
	expectedApp.Status.Conditions = []shipper.ApplicationCondition{
		{
			Type:   shipper.ApplicationConditionTypeAborting,
			Status: corev1.ConditionFalse,
		},
		{
			Type:   shipper.ApplicationConditionTypeReleaseSynced,
			Status: corev1.ConditionTrue,
		},
		{
			Type:    shipper.ApplicationConditionTypeRollingOut,
			Status:  corev1.ConditionTrue,
			Message: fmt.Sprintf(InitialReleaseMessageFormat, expectedRelName),
		},
		{
			Type:   shipper.ApplicationConditionTypeValidHistory,
			Status: corev1.ConditionTrue,
		},
	}
	expectedApp.Status.History = []string{expectedRelName}

	// We do not expect entries in the history or 'RollingOut: true' in the state
	// because the testing client does not update listers after Create actions.

	expectedRelease := newRelease(expectedRelName, app)
	expectedRelease.Spec.Environment.Chart.RepoURL = "127.0.0.1"
	expectedRelease.Labels[shipper.ReleaseEnvironmentHashLabel] = envHash
	expectedRelease.Annotations[shipper.ReleaseTemplateIterationAnnotation] = "0"
	expectedRelease.Annotations[shipper.ReleaseGenerationAnnotation] = "0"

	f.expectReleaseCreate(expectedRelease)
	f.expectApplicationUpdate(expectedApp)
	f.run()
}

func TestStatusStableState(t *testing.T) {
	f := newFixture(t)
	app := newApplication(testAppName)

	envHashA := hashReleaseEnvironment(app.Spec.Template)
	expectedRelNameA := fmt.Sprintf("%s-%s-0", testAppName, envHashA)
	releaseA := newRelease(expectedRelNameA, app)
	releaseA.Annotations[shipper.ReleaseGenerationAnnotation] = "0"
	releaseA.Spec.TargetStep = 2
	releaseA.Status.AchievedStep = &shipper.AchievedStep{
		Step: 2,
		Name: releaseA.Spec.Environment.Strategy.Steps[2].Name,
	}

	releaseA.Status.Conditions = []shipper.ReleaseCondition{
		{Type: shipper.ReleaseConditionTypeInstalled, Status: corev1.ConditionTrue},
		{Type: shipper.ReleaseConditionTypeComplete, Status: corev1.ConditionTrue},
	}

	app.Annotations[shipper.AppHighestObservedGenerationAnnotation] = "1"
	app.Spec.Template.Chart.RepoURL = "http://localhost"
	envHashB := hashReleaseEnvironment(app.Spec.Template)
	expectedRelNameB := fmt.Sprintf("%s-%s-0", testAppName, envHashB)
	releaseB := newRelease(expectedRelNameB, app)
	releaseB.Annotations[shipper.ReleaseGenerationAnnotation] = "1"
	releaseB.Spec.TargetStep = 2
	releaseB.Status.AchievedStep = &shipper.AchievedStep{
		Step: 2,
		Name: releaseB.Spec.Environment.Strategy.Steps[2].Name,
	}

	releaseB.Status.Conditions = []shipper.ReleaseCondition{
		{Type: shipper.ReleaseConditionTypeInstalled, Status: corev1.ConditionTrue},
		{Type: shipper.ReleaseConditionTypeComplete, Status: corev1.ConditionTrue},
	}

	f.objects = append(f.objects, app, releaseA, releaseB)
	expectedApp := app.DeepCopy()
	expectedApp.Status.History = []string{
		expectedRelNameA,
		expectedRelNameB,
	}
	expectedApp.Status.Conditions = []shipper.ApplicationCondition{
		{
			Type:   shipper.ApplicationConditionTypeAborting,
			Status: corev1.ConditionFalse,
		},
		{
			Type:   shipper.ApplicationConditionTypeReleaseSynced,
			Status: corev1.ConditionTrue,
		},
		{
			Type:    shipper.ApplicationConditionTypeRollingOut,
			Status:  corev1.ConditionFalse,
			Message: fmt.Sprintf(ReleaseActiveMessageFormat, expectedRelNameB),
		},
		{
			Type:   shipper.ApplicationConditionTypeValidHistory,
			Status: corev1.ConditionTrue,
		},
	}

	f.expectApplicationUpdate(expectedApp)
	f.run()
}

func TestRevisionHistoryLimit(t *testing.T) {
	f := newFixture(t)
	app := newApplication(testAppName)
	one := int32(1)
	app.Spec.RevisionHistoryLimit = &one
	f.objects = append(f.objects, app)

	releaseFoo := newRelease("foo", app)
	releaseFoo.Spec.TargetStep = 2
	releaseFoo.Status.AchievedStep = &shipper.AchievedStep{
		Step: 2,
		Name: releaseFoo.Spec.Environment.Strategy.Steps[2].Name,
	}
	releaseutil.SetReleaseCondition(&releaseFoo.Status, *releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeComplete, corev1.ConditionTrue, "", ""))
	releaseutil.SetGeneration(releaseFoo, 0)

	releaseBar := newRelease("bar", app)
	releaseBar.Spec.TargetStep = 2
	releaseBar.Status.AchievedStep = &shipper.AchievedStep{
		Step: 2,
		Name: releaseBar.Spec.Environment.Strategy.Steps[2].Name,
	}
	releaseutil.SetReleaseCondition(&releaseBar.Status, *releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeComplete, corev1.ConditionTrue, "", ""))
	releaseutil.SetGeneration(releaseBar, 1)

	releaseBaz := newRelease("baz", app)
	releaseBaz.Spec.TargetStep = 2
	releaseBaz.Status.AchievedStep = &shipper.AchievedStep{
		Step: 2,
		Name: releaseBaz.Spec.Environment.Strategy.Steps[2].Name,
	}
	releaseutil.SetReleaseCondition(&releaseBaz.Status, *releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeComplete, corev1.ConditionTrue, "", ""))
	releaseutil.SetGeneration(releaseBaz, 2)

	f.objects = append(f.objects, releaseFoo, releaseBar, releaseBaz)

	app.Status.History = []string{"foo", "bar", "baz"}

	expectedApp := app.DeepCopy()
	expectedApp.Annotations[shipper.AppHighestObservedGenerationAnnotation] = "2"

	// This ought to be true, but deletes don't filter through the kubetesting
	// lister.

	//expectedApp.Status.History = []string{"baz"}
	expectedApp.Status.Conditions = []shipper.ApplicationCondition{
		{
			Type:   shipper.ApplicationConditionTypeAborting,
			Status: corev1.ConditionFalse,
		},
		{
			Type:   shipper.ApplicationConditionTypeReleaseSynced,
			Status: corev1.ConditionTrue,
		},
		{
			Type:    shipper.ApplicationConditionTypeRollingOut,
			Status:  corev1.ConditionFalse,
			Message: fmt.Sprintf(ReleaseActiveMessageFormat, releaseBaz.Name),
		},
		{
			Type:   shipper.ApplicationConditionTypeValidHistory,
			Status: corev1.ConditionTrue,
		},
	}

	f.expectReleaseDelete(releaseFoo)
	f.expectApplicationUpdate(expectedApp)
	f.run()
}

func TestCreateThirdRelease(t *testing.T) {
	srv, hh, err := repotest.NewTempServer("testdata/*.tgz")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		os.RemoveAll(hh.String())
		srv.Stop()
	}()

	f := newFixture(t)
	app := newApplication(testAppName)
	apputil.SetHighestObservedGeneration(app, 1)
	app.Spec.Template.Chart.RepoURL = srv.URL()

	incumbentEnvHash := hashReleaseEnvironment(app.Spec.Template)

	firstRelName := fmt.Sprintf("%s-%s-0", testAppName, incumbentEnvHash)
	firstRel := newRelease(firstRelName, app)
	releaseutil.SetIteration(firstRel, 0)
	releaseutil.SetGeneration(firstRel, 0)
	releaseutil.SetReleaseCondition(&firstRel.Status, *releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeComplete, corev1.ConditionTrue, "", ""))
	firstRel.Spec.Environment.Chart.RepoURL = srv.URL()
	firstRel.Spec.TargetStep = 2
	firstRel.Status.AchievedStep = &shipper.AchievedStep{
		Step: 2,
		Name: firstRel.Spec.Environment.Strategy.Steps[2].Name,
	}

	incumbentRelName := fmt.Sprintf("%s-%s-1", testAppName, incumbentEnvHash)
	incumbentRel := newRelease(incumbentRelName, app)
	releaseutil.SetIteration(incumbentRel, 1)
	releaseutil.SetGeneration(incumbentRel, 1)
	releaseutil.SetReleaseCondition(&incumbentRel.Status, *releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeComplete, corev1.ConditionTrue, "", ""))
	incumbentRel.Spec.Environment.Chart.RepoURL = srv.URL()
	incumbentRel.Spec.TargetStep = 2
	incumbentRel.Status.AchievedStep = &shipper.AchievedStep{
		Step: 2,
		Name: incumbentRel.Spec.Environment.Strategy.Steps[2].Name,
	}

	app.Status.History = []string{firstRelName, incumbentRelName}
	app.Spec.Template.ClusterRequirements = shipper.ClusterRequirements{
		Regions: []shipper.RegionRequirement{{Name: "foo"}},
	}

	f.objects = append(f.objects, app, firstRel, incumbentRel)

	contenderEnvHash := hashReleaseEnvironment(app.Spec.Template)
	expectedContenderRelName := fmt.Sprintf("%s-%s-0", testAppName, contenderEnvHash)

	expectedContenderRel := newRelease(expectedContenderRelName, app)
	expectedContenderRel.Labels[shipper.ReleaseEnvironmentHashLabel] = contenderEnvHash
	releaseutil.SetIteration(expectedContenderRel, 0)
	releaseutil.SetGeneration(expectedContenderRel, 2)

	expectedApp := app.DeepCopy()
	apputil.SetHighestObservedGeneration(expectedApp, 2)
	expectedApp.Status.History = []string{
		firstRelName,
		incumbentRelName,
		expectedContenderRelName,
	}

	expectedApp.Status.Conditions = []shipper.ApplicationCondition{
		{
			Type:   shipper.ApplicationConditionTypeAborting,
			Status: corev1.ConditionFalse,
		},
		{
			Type:   shipper.ApplicationConditionTypeReleaseSynced,
			Status: corev1.ConditionTrue,
		},
		{
			Type:    shipper.ApplicationConditionTypeRollingOut,
			Status:  corev1.ConditionTrue,
			Message: fmt.Sprintf(TransitioningMessageFormat, incumbentRelName, expectedContenderRelName),
		},
		{
			Type:   shipper.ApplicationConditionTypeValidHistory,
			Status: corev1.ConditionTrue,
		},
	}

	f.expectReleaseCreate(expectedContenderRel)
	f.expectApplicationUpdate(expectedApp)
	f.run()
}

// An app with 1 existing release should create a new one when its template has
// changed.
func TestCreateSecondRelease(t *testing.T) {
	srv, hh, err := repotest.NewTempServer("testdata/*.tgz")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		os.RemoveAll(hh.String())
		srv.Stop()
	}()

	f := newFixture(t)
	app := newApplication(testAppName)
	apputil.SetHighestObservedGeneration(app, 0)
	app.Spec.Template.Chart.RepoURL = srv.URL()
	f.objects = append(f.objects, app)

	incumbentEnvHash := hashReleaseEnvironment(app.Spec.Template)
	incumbentRelName := fmt.Sprintf("%s-%s-0", testAppName, incumbentEnvHash)

	incumbentRel := newRelease(incumbentRelName, app)
	releaseutil.SetGeneration(incumbentRel, 0)
	releaseutil.SetIteration(incumbentRel, 0)
	releaseutil.SetReleaseCondition(&incumbentRel.Status, *releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeComplete, corev1.ConditionTrue, "", ""))
	incumbentRel.Spec.Environment.Chart.RepoURL = srv.URL()
	incumbentRel.Spec.TargetStep = 2
	incumbentRel.Status.AchievedStep = &shipper.AchievedStep{
		Step: 2,
		Name: incumbentRel.Spec.Environment.Strategy.Steps[2].Name,
	}

	f.objects = append(f.objects, incumbentRel)

	app.Status.History = []string{incumbentRelName}
	app.Spec.Template.ClusterRequirements = shipper.ClusterRequirements{
		Regions: []shipper.RegionRequirement{{Name: "foo"}},
	}

	contenderEnvHash := hashReleaseEnvironment(app.Spec.Template)
	contenderRelName := fmt.Sprintf("%s-%s-0", testAppName, contenderEnvHash)

	contenderRel := newRelease(contenderRelName, app)
	contenderRel.Labels[shipper.ReleaseEnvironmentHashLabel] = contenderEnvHash
	releaseutil.SetIteration(contenderRel, 0)
	releaseutil.SetGeneration(contenderRel, 1)

	expectedApp := app.DeepCopy()
	apputil.SetHighestObservedGeneration(expectedApp, 1)
	expectedApp.Status.History = []string{
		incumbentRelName,
		contenderRelName,
	}

	expectedApp.Status.Conditions = []shipper.ApplicationCondition{
		{
			Type:   shipper.ApplicationConditionTypeAborting,
			Status: corev1.ConditionFalse,
		},
		{
			Type:   shipper.ApplicationConditionTypeReleaseSynced,
			Status: corev1.ConditionTrue,
		},
		{
			Type:    shipper.ApplicationConditionTypeRollingOut,
			Status:  corev1.ConditionTrue,
			Message: fmt.Sprintf(TransitioningMessageFormat, incumbentRelName, contenderRelName),
		},
		{
			Type:   shipper.ApplicationConditionTypeValidHistory,
			Status: corev1.ConditionTrue,
		},
	}

	f.expectReleaseCreate(contenderRel)
	f.expectApplicationUpdate(expectedApp)
	f.run()
}

// An app's template should be rolled back to the previous release if the
// previous-highest was deleted.
func TestAbort(t *testing.T) {
	srv, hh, err := repotest.NewTempServer("testdata/*.tgz")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		os.RemoveAll(hh.String())
		srv.Stop()
	}()

	f := newFixture(t)
	app := newApplication(testAppName)
	app.Spec.Template.Chart.RepoURL = srv.URL()
	// Highest observed is higher than any known release. We have an older release
	// (gen 0) with a different cluster selector. We expect app template to be
	// reverted.
	app.Annotations[shipper.AppHighestObservedGenerationAnnotation] = "1"
	app.Spec.Template.ClusterRequirements = shipper.ClusterRequirements{
		Regions: []shipper.RegionRequirement{{Name: "foo"}},
	}

	f.objects = append(f.objects, app)

	envHash := hashReleaseEnvironment(app.Spec.Template)
	relName := fmt.Sprintf("%s-%s-0", testAppName, envHash)

	release := newRelease(relName, app)
	release.Spec.Environment.Chart.RepoURL = srv.URL()
	release.Annotations[shipper.ReleaseGenerationAnnotation] = "0"
	release.Spec.TargetStep = 2
	release.Status.AchievedStep = &shipper.AchievedStep{
		Step: 2,
		Name: release.Spec.Environment.Strategy.Steps[2].Name,
	}

	release.Spec.Environment.ClusterRequirements = shipper.ClusterRequirements{
		Regions: []shipper.RegionRequirement{{Name: "bar"}},
	}

	f.objects = append(f.objects, release)

	app.Status.History = []string{relName, "blorgblorgblorg"}

	expectedApp := app.DeepCopy()
	expectedApp.Annotations[shipper.AppHighestObservedGenerationAnnotation] = "0"
	// Should have overwritten the old template with the generation 0 one.
	expectedApp.Spec.Template = release.Spec.Environment

	expectedApp.Status.History = []string{relName}

	expectedApp.Status.Conditions = []shipper.ApplicationCondition{
		{
			Type:    shipper.ApplicationConditionTypeAborting,
			Status:  corev1.ConditionTrue,
			Reason:  "",
			Message: fmt.Sprintf("abort in progress, returning state to release %q", relName),
		},
		{
			Type:   shipper.ApplicationConditionTypeRollingOut,
			Status: corev1.ConditionTrue,
		},
	}

	f.expectApplicationUpdate(expectedApp)
	f.run()
}

func TestStateRollingOut(t *testing.T) {
	srv, hh, err := repotest.NewTempServer("testdata/*.tgz")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		os.RemoveAll(hh.String())
		srv.Stop()
	}()

	f := newFixture(t)

	// App with two Releases: one installed and running, and one being rolled out.

	app := newApplication(testAppName)
	app.Spec.Template.Chart.RepoURL = srv.URL()
	app.Annotations[shipper.AppHighestObservedGenerationAnnotation] = "1"

	envHash := hashReleaseEnvironment(app.Spec.Template)
	incumbentName := fmt.Sprintf("%s-%s-0", testAppName, envHash)
	contenderName := fmt.Sprintf("%s-%s-1", testAppName, envHash)
	app.Status.History = []string{incumbentName, contenderName}

	f.objects = append(f.objects, app)

	incumbent := newRelease(incumbentName, app)
	incumbent.Annotations[shipper.ReleaseGenerationAnnotation] = "0"
	incumbent.Status.Conditions = []shipper.ReleaseCondition{
		{Type: shipper.ReleaseConditionTypeInstalled, Status: corev1.ConditionTrue},
		{Type: shipper.ReleaseConditionTypeComplete, Status: corev1.ConditionTrue},
	}
	f.objects = append(f.objects, incumbent)

	contender := newRelease(contenderName, app)
	contender.Annotations[shipper.ReleaseGenerationAnnotation] = "1"
	contender.Spec.TargetStep = 1
	contender.Status.AchievedStep = &shipper.AchievedStep{
		Step: 0,
		Name: contender.Spec.Environment.Strategy.Steps[0].Name,
	}

	f.objects = append(f.objects, contender)

	appRollingOut := app.DeepCopy()

	appRollingOut.Status.Conditions = []shipper.ApplicationCondition{
		{
			Type:   shipper.ApplicationConditionTypeAborting,
			Status: corev1.ConditionFalse,
		},
		{
			Type:   shipper.ApplicationConditionTypeReleaseSynced,
			Status: corev1.ConditionTrue,
		},
		{
			Type:    shipper.ApplicationConditionTypeRollingOut,
			Status:  corev1.ConditionTrue,
			Message: fmt.Sprintf(TransitioningMessageFormat, incumbent.Name, contender.Name),
		},
		{
			Type:   shipper.ApplicationConditionTypeValidHistory,
			Status: corev1.ConditionTrue,
		},
	}

	f.expectApplicationUpdate(appRollingOut)
	f.run()
}

// If a release which is not installed is in the app history and it's not the
// latest release, it should be nuked.
func TestDeletingAbortedReleases(t *testing.T) {
	f := newFixture(t)
	app := newApplication(testAppName)
	f.objects = append(f.objects, app)

	releaseFoo := newRelease("foo", app)
	releaseFoo.Annotations[shipper.ReleaseGenerationAnnotation] = "0"

	releaseBar := newRelease("bar", app)
	releaseutil.SetGeneration(releaseBar, 1)
	releaseutil.SetReleaseCondition(&releaseBar.Status, *releaseutil.NewReleaseCondition(shipper.ReleaseConditionTypeComplete, corev1.ConditionTrue, "", ""))
	releaseBar.Spec.TargetStep = 2
	releaseBar.Status.AchievedStep = &shipper.AchievedStep{
		Step: 2,
		Name: releaseBar.Spec.Environment.Strategy.Steps[2].Name,
	}

	f.objects = append(f.objects, releaseFoo, releaseBar)

	app.Status.History = []string{"foo", "bar"}

	expectedApp := app.DeepCopy()
	expectedApp.Annotations[shipper.AppHighestObservedGenerationAnnotation] = "1"

	expectedApp.Status.Conditions = []shipper.ApplicationCondition{
		{
			Type:   shipper.ApplicationConditionTypeAborting,
			Status: corev1.ConditionFalse,
		},
		{
			Type:   shipper.ApplicationConditionTypeReleaseSynced,
			Status: corev1.ConditionTrue,
		},
		{
			Type:    shipper.ApplicationConditionTypeRollingOut,
			Status:  corev1.ConditionFalse,
			Message: fmt.Sprintf(ReleaseActiveMessageFormat, releaseBar.Name),
		},
		{
			Type:   shipper.ApplicationConditionTypeValidHistory,
			Status: corev1.ConditionTrue,
		},
	}

	f.expectReleaseDelete(releaseFoo)
	f.expectApplicationUpdate(expectedApp)
	f.run()
}

func newRelease(releaseName string, app *shipper.Application) *shipper.Release {
	return &shipper.Release{
		ObjectMeta: metav1.ObjectMeta{
			Name:        releaseName,
			Namespace:   app.GetNamespace(),
			Annotations: map[string]string{},
			Labels: map[string]string{
				shipper.ReleaseLabel: releaseName,
				shipper.AppLabel:     app.GetName(),
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: shipper.SchemeGroupVersion.String(),
					Kind:       "Application",
					Name:       app.GetName(),
				},
			},
		},
		Spec: shipper.ReleaseSpec{
			Environment: *(app.Spec.Template.DeepCopy()),
		},
	}
}

var vanguard = shipper.RolloutStrategy{
	Steps: []shipper.RolloutStrategyStep{
		{
			Name:     "staging",
			Capacity: shipper.RolloutStrategyStepValue{Incumbent: 100, Contender: 1},
			Traffic:  shipper.RolloutStrategyStepValue{Incumbent: 100, Contender: 0},
		},
		{
			Name:     "50/50",
			Capacity: shipper.RolloutStrategyStepValue{Incumbent: 50, Contender: 50},
			Traffic:  shipper.RolloutStrategyStepValue{Incumbent: 50, Contender: 50},
		},
		{
			Name:     "full on",
			Capacity: shipper.RolloutStrategyStepValue{Incumbent: 0, Contender: 100},
			Traffic:  shipper.RolloutStrategyStepValue{Incumbent: 0, Contender: 100},
		},
	},
}

func newApplication(name string) *shipper.Application {
	five := int32(5)
	return &shipper.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   shippertesting.TestNamespace,
			Annotations: map[string]string{},
		},
		Spec: shipper.ApplicationSpec{
			RevisionHistoryLimit: &five,
			Template: shipper.ReleaseEnvironment{
				Chart: shipper.Chart{
					Name:    "simple",
					Version: "0.0.1",
					RepoURL: "http://127.0.0.1:8879/charts",
				},
				ClusterRequirements: shipper.ClusterRequirements{},
				Values:              &shipper.ChartValues{},
				Strategy:            &vanguard,
			},
		},
	}
}

type fixture struct {
	t       *testing.T
	client  *shipperfake.Clientset
	actions []kubetesting.Action
	objects []runtime.Object
}

func newFixture(t *testing.T) *fixture {
	return &fixture{t: t}
}

func (f *fixture) newController() (*Controller, shipperinformers.SharedInformerFactory) {
	f.client = shipperfake.NewSimpleClientset(f.objects...)

	const noResyncPeriod time.Duration = 0
	shipperInformerFactory := shipperinformers.NewSharedInformerFactory(f.client, noResyncPeriod)

	c := NewController(f.client, shipperInformerFactory, record.NewFakeRecorder(42))

	return c, shipperInformerFactory
}

func (f *fixture) run() {
	c, i := f.newController()

	stopCh := make(chan struct{})
	defer close(stopCh)

	i.Start(stopCh)
	i.WaitForCacheSync(stopCh)

	wait.PollUntil(
		10*time.Millisecond,
		func() (bool, error) { return c.appWorkqueue.Len() >= 1, nil },
		stopCh,
	)

	c.processNextWorkItem()

	actual := shippertesting.FilterActions(f.client.Actions())
	shippertesting.CheckActions(f.actions, actual, f.t)
}

func (f *fixture) expectReleaseCreate(rel *shipper.Release) {
	gvr := shipper.SchemeGroupVersion.WithResource("releases")
	action := kubetesting.NewCreateAction(gvr, rel.GetNamespace(), rel)

	f.actions = append(f.actions, action)
}

func (f *fixture) expectReleaseDelete(rel *shipper.Release) {
	gvr := shipper.SchemeGroupVersion.WithResource("releases")
	action := kubetesting.NewDeleteAction(gvr, rel.GetNamespace(), rel.GetName())

	f.actions = append(f.actions, action)
}

func (f *fixture) expectApplicationUpdate(app *shipper.Application) {
	gvr := shipper.SchemeGroupVersion.WithResource("applications")
	action := kubetesting.NewUpdateAction(gvr, app.GetNamespace(), app)

	f.actions = append(f.actions, action)
}
