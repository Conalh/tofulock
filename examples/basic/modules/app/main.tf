# Placeholder local module referenced by ../../main.tf (module "local_app").
variable "name" {
  type    = string
  default = "app"
}

output "name" {
  value = var.name
}
