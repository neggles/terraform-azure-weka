terraform {
  backend "azurerm" {
    resource_group_name  = "weka-tf"
    storage_account_name = "wekatfbackendsa"
    container_name       = "tfstate"
    key                  = "dev-repo.terraform.tfstate"
  }
}