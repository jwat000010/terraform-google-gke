package test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gruntwork-io/terratest/modules/gcp"
	"github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/gruntwork-io/terratest/modules/logger"
	"github.com/gruntwork-io/terratest/modules/random"
	"github.com/gruntwork-io/terratest/modules/shell"
	"github.com/gruntwork-io/terratest/modules/terraform"
	test_structure "github.com/gruntwork-io/terratest/modules/test-structure"
	"github.com/stretchr/testify/require"
)

func TestGKECluster(t *testing.T) {
	t.Parallel()

	var testcases = []struct {
		testName          string
		exampleFolder     string
		overrideDefaultSA bool
	}{
		{
			"PublicCluster",
			"gke-public-cluster",
			false,
		},
		{
			"PrivateCluster",
			"gke-private-cluster",
			false,
		},
		{
			"PublicClusterWithCustomSA",
			"gke-public-cluster",
			true,
		},
	}

	for _, testCase := range testcases {
		// The following is necessary to make sure testCase's values don't
		// get updated due to concurrency within the scope of t.Run(..) below
		testCase := testCase

		t.Run(testCase.testName, func(t *testing.T) {
			t.Parallel()

			// Uncomment any of the following to skip that section during the test
			//os.Setenv("SKIP_create_test_copy_of_examples", "true")
			//os.Setenv("SKIP_create_terratest_options", "true")
			//os.Setenv("SKIP_terraform_apply", "true")
			//os.Setenv("SKIP_configure_kubectl", "true")
			//os.Setenv("SKIP_wait_for_workers", "true")
			//os.Setenv("SKIP_cleanup", "true")

			// Create a directory path that won't conflict
			workingDir := filepath.Join(".", "stages", testCase.testName)

			test_structure.RunTestStage(t, "create_test_copy_of_examples", func() {
				testFolder := test_structure.CopyTerraformFolderToTemp(t, "..", "examples")
				logger.Logf(t, "path to test folder %s\n", testFolder)
				terraformModulePath := filepath.Join(testFolder, testCase.exampleFolder)
				test_structure.SaveString(t, workingDir, "gkeClusterTerraformModulePath", terraformModulePath)
			})

			test_structure.RunTestStage(t, "create_terratest_options", func() {
				gkeClusterTerraformModulePath := test_structure.LoadString(t, workingDir, "gkeClusterTerraformModulePath")
				tmpKubeConfigPath := k8s.CopyHomeKubeConfigToTemp(t)
				kubectlOptions := k8s.NewKubectlOptions("", tmpKubeConfigPath)
				uniqueID := random.UniqueId()
				project := gcp.GetGoogleProjectIDFromEnvVar(t)
				region := gcp.GetRandomRegion(t, project, nil, nil)
				gkeClusterTerratestOptions := createTestGKEClusterTerraformOptions(t, uniqueID, project, region, region, gkeClusterTerraformModulePath)
				if testCase.overrideDefaultSA {
					gkeClusterTerratestOptions.Vars["override_default_node_pool_service_account"] = "1"
				}
				test_structure.SaveString(t, workingDir, "uniqueID", uniqueID)
				test_structure.SaveString(t, workingDir, "project", project)
				test_structure.SaveString(t, workingDir, "location", region)
				test_structure.SaveString(t, workingDir, "region", region)
				test_structure.SaveTerraformOptions(t, workingDir, gkeClusterTerratestOptions)
				test_structure.SaveKubectlOptions(t, workingDir, kubectlOptions)
			})

			defer test_structure.RunTestStage(t, "cleanup", func() {
				gkeClusterTerratestOptions := test_structure.LoadTerraformOptions(t, workingDir)
				terraform.Destroy(t, gkeClusterTerratestOptions)

				kubectlOptions := test_structure.LoadKubectlOptions(t, workingDir)
				err := os.Remove(kubectlOptions.ConfigPath)
				require.NoError(t, err)
			})

			test_structure.RunTestStage(t, "terraform_apply", func() {
				gkeClusterTerratestOptions := test_structure.LoadTerraformOptions(t, workingDir)
				terraform.InitAndApply(t, gkeClusterTerratestOptions)
			})

			test_structure.RunTestStage(t, "configure_kubectl", func() {
				gkeClusterTerratestOptions := test_structure.LoadTerraformOptions(t, workingDir)
				kubectlOptions := test_structure.LoadKubectlOptions(t, workingDir)
				project := test_structure.LoadString(t, workingDir, "project")
				location := test_structure.LoadString(t, workingDir, "location")
				clusterName := gkeClusterTerratestOptions.Vars["cluster_name"].(string)

				// gcloud beta container clusters get-credentials example-cluster --region australia-southeast1 --project dev-sandbox-123456
				cmd := shell.Command{
					Command: "gcloud",
					Args:    []string{"beta", "container", "clusters", "get-credentials", clusterName, "--region", location, "--project", project},
					Env: map[string]string{
						"KUBECONFIG": kubectlOptions.ConfigPath,
					},
				}

				shell.RunCommand(t, cmd)
			})

			test_structure.RunTestStage(t, "wait_for_workers", func() {
				kubectlOptions := test_structure.LoadKubectlOptions(t, workingDir)
				verifyGkeNodesAreReady(t, kubectlOptions)
			})
		})
	}
}
