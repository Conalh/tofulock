# Example Terragrunt unit. tofulock reads the module source from the
# terraform{} block and pins/verifies/attests it like any other module:
#   tofulock lock   ./examples/terragrunt
#   tofulock verify ./examples/terragrunt
terraform {
  source = "git::https://github.com/terraform-aws-modules/terraform-aws-vpc.git//?ref=v5.8.1"
}

# Other Terragrunt blocks (include, inputs, dependency, ...) are ignored for
# module pinning. A `tfr://...?version=x` source would be resolved through the
# module registry instead.
inputs = {
  name = "example"
}
