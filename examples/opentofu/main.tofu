# OpenTofu config (.tofu file): one git-pinned module and one bare registry
# address, locked against registry.opentofu.org (see README "OpenTofu").

module "vpc_git" {
  source = "git::https://github.com/terraform-aws-modules/terraform-aws-vpc.git?ref=v5.8.1"
}

module "vpc_registry" {
  source  = "terraform-aws-modules/vpc/aws"
  version = "6.6.1"
}
