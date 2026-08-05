# Code from LUDUS ESC13 role
# Licence GPL-3.0
# https://github.com/badsectorlabs/ludus_adcs/blob/main/files/esc13.ps1

param(
    [Parameter(Mandatory=$true)]
    [string]$esc13group,

    [Parameter(Mandatory=$true)]
    [string]$esc13templateName
)


# Import modules (just in case)
import-module ADCSTemplate
import-module ActiveDirectory

 # Function to generate a random hexadecimal string of a given length
 Function Get-RandomHex {
    param ([int]$Length)
    $Hex = '0123456789ABCDEF'
    $Return = ''
    1..$Length | ForEach-Object {
        $Return += $Hex.Substring((Get-Random -Minimum 0 -Maximum 16),1)
    }
    Return $Return
}

# Function to check if a given OID is unique
Function IsUniqueOID {
    param ($cn, $TemplateOID, $ConfigNC)
    $Search = Get-ADObject -Filter {cn -eq $cn -and msPKI-Cert-Template-OID -eq $TemplateOID} -SearchBase "CN=OID,CN=Public Key Services,CN=Services,$ConfigNC"
    If ($Search) {$False} Else {$True}
}

# Function to generate a new unique OID
Function New-TemplateOID {
    Param($ConfigNC)
    do {
        $OID_Part_1 = Get-Random -Minimum 10000000 -Maximum 99999999
        $OID_Part_2 = Get-Random -Minimum 10000000 -Maximum 99999999
        $OID_Part_3 = Get-RandomHex -Length 32
        $OID_Forest = Get-ADObject -Identity "CN=OID,CN=Public Key Services,CN=Services,$ConfigNC" -Properties msPKI-Cert-Template-OID |
            Select-Object -ExpandProperty msPKI-Cert-Template-OID
        $msPKICertTemplateOID = "$OID_Forest.$OID_Part_1.$OID_Part_2"
        $Name = "$OID_Part_2.$OID_Part_3"
    } until (IsUniqueOID -cn $Name -TemplateOID $msPKICertTemplateOID -ConfigNC $ConfigNC)
    Return @{
        TemplateOID  = $msPKICertTemplateOID
        TemplateName = $Name
    }
}

# Get the configuration naming context
$ADRootDSE = Get-ADRootDSE
$ConfigNC = $ADRootDSE.configurationNamingContext

# Define the display name and the template
$IssuanceName = "IssuancePolicyESC13"
$ESC13Template = "CN=$esc13templateName,CN=Certificate Templates,CN=Public Key Services,CN=Services,$ConfigNC"

# Define the path to the OID
$TemplateOIDPath = "CN=OID,CN=Public Key Services,CN=Services,$ConfigNC"

# Reuse the issuance policy OID if this already ran. Creating one unconditionally
# leaves a second, unlinked OID object behind on every re-run (lab reset), points
# the template at both, and links only one of them.
# @() keeps a single match from unrolling to a bare string, whose [0] would be the
# character "C" rather than a distinguished name.
$existing = @(Get-ADObject -SearchBase $TemplateOIDPath -Filter { DisplayName -eq $IssuanceName } -Properties DisplayName, 'msPKI-Cert-Template-OID')

if ($existing.Count -gt 1) {
    # Converge the duplicate state an earlier run may have left behind.
    Write-Output "Removing $($existing.Count - 1) duplicate $IssuanceName OID object(s)"
    $existing | Select-Object -Skip 1 | ForEach-Object {
        Remove-ADObject -Identity $_.DistinguishedName -Confirm:$false
    }
    $existing = @($existing | Select-Object -First 1)
}

if ($existing.Count -eq 0) {
    $OID = New-TemplateOID -ConfigNC $ConfigNC
    $oa = @{
        'DisplayName' = $IssuanceName
        'Name' = $IssuanceName
        'flags' = [System.Int32]'2'
        'msPKI-Cert-Template-OID' = $OID.TemplateOID
    }
    New-ADObject -Path $TemplateOIDPath -OtherAttributes $oa -Name $OID.TemplateName -Type 'msPKI-Enterprise-Oid'
    $existing = @(Get-ADObject -SearchBase $TemplateOIDPath -Filter { DisplayName -eq $IssuanceName } -Properties DisplayName, 'msPKI-Cert-Template-OID')
}

$newOIDObj = $existing | Select-Object -First 1
$newOIDValue = $newOIDObj.'msPKI-Cert-Template-OID'
$esc13OID_dn = $newOIDObj.DistinguishedName
if (-not $esc13OID_dn) {
    throw "Could not resolve the $IssuanceName OID object under $TemplateOIDPath"
}
$esc13OID_dn

# Point the ESC13 template at exactly this issuance policy
$adObject = Get-ADObject $ESC13Template -Properties msPKI-Certificate-Policy
Set-ADObject -Identity $adObject.DistinguishedName -Replace @{ 'msPKI-Certificate-Policy' = $newOIDValue.ToString() }

# Get DN of the ESC13 Group
$ludus_esc13_group_dn = (Get-ADGroup $esc13group).DistinguishedName
$ludus_esc13_group_dn

# Create a DirectoryEntry object for the Issuance Policy OID
# Thanks to Jonas (https://twitter.com/Jonas_B_K) for helping with this!
$object = New-Object System.DirectoryServices.DirectoryEntry("LDAP://$esc13OID_dn")

# Set the msDS-OIDToGroupLink property to the DN of the ESC13 group
$Toset = $ludus_esc13_group_dn
$object.Properties["msDS-OIDToGroupLink"].Value = $Toset
$object.CommitChanges()
$object.RefreshCache()
$object | select msDS-OIDToGroupLink

# The group link is what makes ESC13 exploitable, and a silent no-op here looks
# identical to success, so confirm it landed in the directory.
$link = Get-ADObject -Identity $esc13OID_dn -Properties 'msDS-OIDToGroupLink' |
    Select-Object -ExpandProperty 'msDS-OIDToGroupLink'
if ($link -ne $ludus_esc13_group_dn) {
    throw "msDS-OIDToGroupLink on $esc13OID_dn is '$link', expected '$ludus_esc13_group_dn'"
}
Write-Output "ESC13 linked: $esc13OID_dn -> $link"
