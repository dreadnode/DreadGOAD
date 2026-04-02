$ProgressPreference = 'SilentlyContinue'
$ErrorActionPreference = 'Stop'

Write-Host "Installing NuGet provider..."
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
Install-PackageProvider -Name NuGet -Force -Confirm:$false

Write-Host "Installing PowerShellGet module..."
Install-Module PowerShellGet -Force -Confirm:$false

Write-Host "Installing required DSC modules..."
$modules = @('ComputerManagementDsc', 'ActiveDirectoryDsc', 'xNetworking', 'NetworkingDsc')
foreach ($module in $modules) {
    Write-Host "Installing module: $module"
    Install-Module -Name $module -Force -Confirm:$false -SkipPublisherCheck -AcceptLicense
}

Write-Host "PowerShell modules installed successfully"
