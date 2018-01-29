package installation

import (
	shipperV1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
)

func (c *Controller) renderChart(chrt shipperV1.EmbeddedChart, cluster *shipperV1.Cluster) ([]string, error) {
	// TODO: Implement chart rendering
	// - Render chart for cluster
	return []string{}, nil
}

func (c *Controller) installIfMissing(rendered []string, cluster *shipperV1.Cluster) error {
	// - Check if rendered objects already exist in target cluster
	//   - If do not exist, create them (Research how smart Tiller is here)
	// - Install rendered objects in target cluster
	return nil
}

func (c *Controller) clusterBusinessLogic(
	release *shipperV1.Release,
	it *shipperV1.InstallationTarget,
	cluster *shipperV1.Cluster,
) error {
	rendered, err := c.renderChart(release.Environment.Chart, cluster)
	if err != nil {
		return err
	}

	err = c.installIfMissing(rendered, cluster)
	if err != nil {
		return err
	}
	return nil
}

func (c *Controller) businessLogic(it *shipperV1.InstallationTarget) error {

	release, err := c.releaseLister.Releases(it.Namespace).Get(it.Name)
	if err != nil {
		return err
	}

	// Iterate list of target clusters:
	for _, clusterName := range it.Spec.Clusters {
		cluster, err := c.clusterLister.Get(clusterName)
		if err != nil {
			return err
		}

		if err = c.clusterBusinessLogic(release, it, cluster); err != nil {
			return err
		}
	}

	_, err = c.shipperclientset.ShipperV1().InstallationTargets(it.Namespace).Update(it)
	if err != nil {
		return err
	}

	return nil
}
