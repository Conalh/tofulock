# Example root module exercising every source kind tofulock classifies.
#
#   tofulock list   .
#   tofulock lock   .
#   tofulock verify .

# Registry source — version-constrained but NOT pinned by the native
# .terraform.lock.hcl. tofulock records it as skipped today (roadmap).
module "vpc_registry" {
  source  = "terraform-aws-modules/vpc/aws"
  version = "5.8.1"
}

# Git source via forced getter + subdir + ref. tofulock pins this to a commit.
module "vpc_git" {
  source = "git::https://github.com/terraform-aws-modules/terraform-aws-vpc.git//?ref=v5.8.1"
}

# Git source via detector shorthand + ref. Also pinned to a commit.
module "network" {
  source = "github.com/terraform-aws-modules/terraform-aws-s3-bucket?ref=v4.1.2"
}

# Local source — versioned with the root module, so tofulock skips it.
module "local_app" {
  source = "./modules/app"
}
