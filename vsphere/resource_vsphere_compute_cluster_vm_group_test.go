package vsphere

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sort"
	"testing"

	"github.com/davecgh/go-spew/spew"
	"github.com/hashicorp/terraform/helper/resource"
	"github.com/hashicorp/terraform/terraform"
	"github.com/terraform-providers/terraform-provider-vsphere/vsphere/internal/helper/structure"
	"github.com/terraform-providers/terraform-provider-vsphere/vsphere/internal/helper/viapi"
	"github.com/terraform-providers/terraform-provider-vsphere/vsphere/internal/helper/virtualmachine"
	"github.com/vmware/govmomi/vim25/types"
)

func TestAccResourceVSphereComputeClusterVMGroup_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testAccResourceVSphereComputeClusterVMGroupPreCheck(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccResourceVSphereComputeClusterVMGroupExists(false),
		Steps: []resource.TestStep{
			{
				Config: testAccResourceVSphereComputeClusterVMGroupConfig(2),
				Check: resource.ComposeTestCheckFunc(
					testAccResourceVSphereComputeClusterVMGroupExists(true),
					testAccResourceVSphereComputeClusterVMGroupMatchMembership(),
				),
			},
		},
	})
}

func TestAccResourceVSphereComputeClusterVMGroup_update(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testAccResourceVSphereComputeClusterVMGroupPreCheck(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccResourceVSphereComputeClusterVMGroupExists(false),
		Steps: []resource.TestStep{
			{
				Config: testAccResourceVSphereComputeClusterVMGroupConfig(2),
				Check: resource.ComposeTestCheckFunc(
					testAccResourceVSphereComputeClusterVMGroupExists(true),
					testAccResourceVSphereComputeClusterVMGroupMatchMembership(),
				),
			},
			{
				Config: testAccResourceVSphereComputeClusterVMGroupConfig(3),
				Check: resource.ComposeTestCheckFunc(
					testAccResourceVSphereComputeClusterVMGroupExists(true),
					testAccResourceVSphereComputeClusterVMGroupMatchMembership(),
				),
			},
		},
	})
}

func TestAccResourceVSphereComputeClusterVMGroup_import(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			testAccResourceVSphereComputeClusterVMGroupPreCheck(t)
		},
		Providers:    testAccProviders,
		CheckDestroy: testAccResourceVSphereComputeClusterVMGroupExists(false),
		Steps: []resource.TestStep{
			{
				Config: testAccResourceVSphereComputeClusterVMGroupConfig(1),
				Check: resource.ComposeTestCheckFunc(
					testAccResourceVSphereComputeClusterVMGroupExists(true),
					testAccResourceVSphereComputeClusterVMGroupMatchMembership(),
				),
			},
			{
				ResourceName:      "vsphere_compute_cluster_vm_group.cluster_vm_group",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					cluster, err := testGetComputeClusterFromDataSource(s, "cluster")
					if err != nil {
						return "", err
					}

					rs, ok := s.RootModule().Resources["vsphere_compute_cluster_vm_group.cluster_vm_group"]
					if !ok {
						return "", errors.New("no resource at address vsphere_compute_cluster_vm_group.cluster_vm_group")
					}
					name, ok := rs.Primary.Attributes["name"]
					if !ok {
						return "", errors.New("vsphere_compute_cluster_vm_group.cluster_vm_group has no name attribute")
					}

					m := make(map[string]string)
					m["compute_cluster_path"] = cluster.InventoryPath
					m["name"] = name
					b, err := json.Marshal(m)
					if err != nil {
						return "", err
					}

					return string(b), nil
				},
				Config: testAccResourceVSphereComputeClusterVMGroupConfig(1),
				Check: resource.ComposeTestCheckFunc(
					testAccResourceVSphereComputeClusterVMGroupExists(true),
					testAccResourceVSphereComputeClusterVMGroupMatchMembership(),
				),
			},
		},
	})
}

func testAccResourceVSphereComputeClusterVMGroupPreCheck(t *testing.T) {
	if os.Getenv("VSPHERE_DATACENTER") == "" {
		t.Skip("set VSPHERE_DATACENTER to run vsphere_storage_drs_vm_override acceptance tests")
	}
	if os.Getenv("VSPHERE_DATASTORE") == "" {
		t.Skip("set VSPHERE_DATASTORE to run vsphere_storage_drs_vm_override acceptance tests")
	}
	if os.Getenv("VSPHERE_CLUSTER") == "" {
		t.Skip("set VSPHERE_CLUSTER to run vsphere_storage_drs_vm_override acceptance tests")
	}
	if os.Getenv("VSPHERE_NETWORK_LABEL_PXE") == "" {
		t.Skip("set VSPHERE_NETWORK_LABEL_PXE to run vsphere_storage_drs_vm_override acceptance tests")
	}
}

func testAccResourceVSphereComputeClusterVMGroupExists(expected bool) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		info, err := testGetComputeClusterVMGroup(s, "cluster_vm_group")
		if err != nil {
			if expected == false {
				if viapi.IsManagedObjectNotFoundError(err) {
					// This is not necessarily a missing group, but more than likely a
					// missing cluster, which happens during destroy as the dependent
					// resources will be missing as well, so want to treat this as a
					// deleted override as well.
					return nil
				}
			}
			return err
		}

		switch {
		case info == nil && !expected:
			// Expected missing
			return nil
		case info == nil && expected:
			// Expected to exist
			return errors.New("cluster VM group missing when expected to exist")
		case !expected:
			return errors.New("cluster VM group still present when expected to be missing")
		}

		return nil
	}
}

func testAccResourceVSphereComputeClusterVMGroupMatchMembership() resource.TestCheckFunc {
	return func(s *terraform.State) error {
		actual, err := testGetComputeClusterVMGroup(s, "cluster_vm_group")
		if err != nil {
			return err
		}

		if actual == nil {
			return errors.New("cluster VM group missing")
		}

		vms, err := testAccResourceVSphereComputeClusterVMGroupMatchMembershipVMIDs(s)
		if err != nil {
			return err
		}

		expectedSort := structure.MoRefSorter(vms)
		sort.Sort(expectedSort)

		expected := &types.ClusterVmGroup{
			ClusterGroupInfo: types.ClusterGroupInfo{
				Name:        actual.Name,
				UserCreated: actual.UserCreated,
			},
			Vm: []types.ManagedObjectReference(expectedSort),
		}

		actualSort := structure.MoRefSorter(actual.Vm)
		sort.Sort(actualSort)
		actual.Vm = []types.ManagedObjectReference(actualSort)

		if !reflect.DeepEqual(expected, actual) {
			return spew.Errorf("expected %#v got %#v", expected, actual)
		}

		return nil
	}
}

func testAccResourceVSphereComputeClusterVMGroupMatchMembershipVMIDs(s *terraform.State) ([]types.ManagedObjectReference, error) {
	var ids []string
	if rs, ok := s.RootModule().Resources["vsphere_virtual_machine.vm"]; ok {
		ids = []string{rs.Primary.ID}
	} else {
		ids = testAccResourceVSphereComputeClusterVMGroupGetMultiple(s)
	}

	results, err := virtualmachine.MOIDsForUUIDs(testAccProvider.Meta().(*VSphereClient).vimClient, ids)
	if err != nil {
		return nil, err
	}
	return results.ManagedObjectReferences(), nil
}

func testAccResourceVSphereComputeClusterVMGroupGetMultiple(s *terraform.State) []string {
	var i int
	var ids []string
	for {
		rs, ok := s.RootModule().Resources[fmt.Sprintf("vsphere_virtual_machine.vm.%d", i)]
		if !ok {
			break
		}
		ids = append(ids, rs.Primary.ID)
		i++
	}
	return ids
}

func testAccResourceVSphereComputeClusterVMGroupConfig(count int) string {
	return fmt.Sprintf(`
variable "datacenter" {
  default = "%s"
}

variable "datastore" {
  default = "%s"
}

variable "cluster" {
  default = "%s"
}

variable "network_label" {
  default = "%s"
}

variable "vm_count" {
  default = "%d"
}

data "vsphere_datacenter" "dc" {
  name = "${var.datacenter}"
}

data "vsphere_datastore" "datastore" {
  name          = "${var.datastore}"
  datacenter_id = "${data.vsphere_datacenter.dc.id}"
}

data "vsphere_compute_cluster" "cluster" {
  name          = "${var.cluster}"
  datacenter_id = "${data.vsphere_datacenter.dc.id}"
}

data "vsphere_network" "network" {
  name          = "${var.network_label}"
  datacenter_id = "${data.vsphere_datacenter.dc.id}"
}

resource "vsphere_virtual_machine" "vm" {
  count            = "${var.vm_count}"
  name             = "terraform-test-${count.index}"
  resource_pool_id = "${data.vsphere_compute_cluster.cluster.resource_pool_id}"
  datastore_id     = "${data.vsphere_datastore.datastore.id}"

  num_cpus = 2
  memory   = 2048
  guest_id = "other3xLinux64Guest"

  wait_for_guest_net_timeout = -1

  network_interface {
    network_id = "${data.vsphere_network.network.id}"
  }

  disk {
    label = "disk0"
    size  = 20
  }
}

resource "vsphere_compute_cluster_vm_group" "cluster_vm_group" {
  name                = "terraform-test-cluster-group"
  compute_cluster_id  = "${data.vsphere_compute_cluster.cluster.id}"
  virtual_machine_ids = ["${vsphere_virtual_machine.vm.*.id}"]
}
`,
		os.Getenv("VSPHERE_DATACENTER"),
		os.Getenv("VSPHERE_DATASTORE"),
		os.Getenv("VSPHERE_CLUSTER"),
		os.Getenv("VSPHERE_NETWORK_LABEL_PXE"),
		count,
	)
}