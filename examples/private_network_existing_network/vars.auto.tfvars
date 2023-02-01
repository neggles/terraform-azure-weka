prefix              = "weka"
rg_name             = "weka-rg"
vnet_name           = "weka-vnet"
subnets_name_list   = ["weka-subnet-0"]
cluster_name        = "poc"
private_network     = true
apt_repo_url        = "http://11.0.0.4/ubuntu/mirror/archive.ubuntu.com/ubuntu/"
install_weka_url    = "https://wekadeploytars.blob.core.windows.net/tars/weka-4.1.0.69-azure.tar"
install_ofed_url    = "https://wekadeploytars.blob.core.windows.net/tars/MLNX_OFED_LINUX-5.7-1.0.2.0-ubuntu18.04-x86_64.tgz"
instance_type       = "Standard_L8s_v3"
set_obs_integration = true
tiering_ssd_percent = 20
cluster_size        = 6