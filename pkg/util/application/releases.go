package application

import (
	releaseutil "github.com/bookingcom/shipper/pkg/util/release"
	shipper "github.com/bookingcom/shipper/pkg/apis/shipper/v1alpha1"
	"github.com/bookingcom/shipper/pkg/errors"
)

// GetContender returns the contender from the given Release slice. The slice
// is expected to be sorted by descending generation.
func GetContender(appName string, rels []*shipper.Release) (*shipper.Release, error) {
	if len(rels) == 0 {
		return nil, errors.NewContenderNotFoundError(appName)
	}
	return rels[0], nil
}

// GetIncumbent returns the incumbent from the given Release slice. The slice
// is expected to be sorted by descending generation.
//
// An incumbent release is the first release in this slice that is considered
// completed.
func GetIncumbent(appName string, rels []*shipper.Release) (*shipper.Release, error) {
	for _, r := range rels {
		if releaseutil.ReleaseComplete(r) {
			return r, nil
		}
	}
	return nil, errors.NewIncumbentNotFoundError(appName)
}

// ReleasesToApplicationHistory transforms the given Release slice into a
// string slice sorted by descending generation, suitable to be used set
// in ApplicationStatus.History.
func ReleasesToApplicationHistory(releases []*shipper.Release) []string {
	releases = releaseutil.SortByGenerationAscending(releases)
	names := make([]string, 0, len(releases))
	for _, rel := range releases {
		names = append(names, rel.GetName())
	}
	return names
}
