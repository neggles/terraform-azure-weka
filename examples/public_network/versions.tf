terraform {
  required_providers {
    azurerm = {
      source  = "hashicorp/azurerm"
      version = "~> 3.26.0"
    }
  }
  required_version = ">= 1.3.7"
}