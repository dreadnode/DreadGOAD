Start-Transcript -Path "C:\setup\mssql\logs\install_transcript.log" -Force

# Define paths
$installerPath = "c:\setup\mssql\sql_installer.exe"
$mediaPath = "c:\setup\mssql\fullmedia"
$logPath = "c:\setup\mssql\logs"
$extractPath = "c:\setup\mssql\extracted"

# Ensure the log directory exists
if (-not (Test-Path $logPath)) {
    New-Item -Path $logPath -ItemType Directory -Force | Out-Null
}

# Ensure extract directory exists
if (-not (Test-Path $extractPath)) {
    New-Item -Path $extractPath -ItemType Directory -Force | Out-Null
}

# Check if this is a web-based installer
$fileInfo = Get-Item -Path $installerPath -ErrorAction SilentlyContinue
$fileSize = $fileInfo.Length
$isWebInstaller = $fileSize -lt 20MB

Write-Host "SQL Server Express installer detected. Size: $fileSize bytes"
if ($isWebInstaller) {
    Write-Host "Installer Type: Web installer"
} else {
    Write-Host "Installer Type: Full installer"
}

# Step 1: Handle the web installer appropriately
if ($isWebInstaller) {
    Write-Host "Web installer detected. First downloading the full installation media..."

    # Create media directory if it doesn't exist
    if (-not (Test-Path $mediaPath)) {
        New-Item -Path $mediaPath -ItemType Directory -Force | Out-Null
    }

    # Use the web installer to download the full installation media
    $downloadArgs = "/ACTION=Download /MEDIAPATH=`"$mediaPath`" /QUIET"
    Write-Host "Running download command: $installerPath $downloadArgs"

    $downloadProcess = Start-Process -FilePath $installerPath -ArgumentList $downloadArgs -Wait -PassThru -NoNewWindow
    $downloadExitCode = $downloadProcess.ExitCode

    Write-Host "Download process completed with exit code: $downloadExitCode"

    if ($downloadExitCode -ne 0) {
        Write-Host "ERROR: Failed to download SQL Server installation media. Exiting."
        Stop-Transcript
        exit $downloadExitCode
    }

    # Look for the downloaded installer - for SQL Express it's typically SQLEXPR_x64_ENU.exe
    $downloadedFiles = Get-ChildItem -Path $mediaPath -File -ErrorAction SilentlyContinue
    foreach ($file in $downloadedFiles) {
        Write-Host "Found downloaded file: $($file.FullName)"
    }

    $sqlExpressInstallers = Get-ChildItem -Path $mediaPath -File -Filter "SQLEXPR*.exe" -ErrorAction SilentlyContinue
    if ($sqlExpressInstallers.Count -gt 0) {
        $setupPath = $sqlExpressInstallers[0].FullName
        Write-Host "Found SQL Express installer at: $setupPath"
    } else {
        # Check for any .exe files
        $exeFiles = Get-ChildItem -Path $mediaPath -File -Filter "*.exe" -ErrorAction SilentlyContinue
        if ($exeFiles.Count -gt 0) {
            $setupPath = $exeFiles[0].FullName
            Write-Host "Found executable file at: $setupPath"
        } else {
            # Look for setup.exe
            $setupFiles = Get-ChildItem -Path $mediaPath -Recurse -Filter "setup.exe" -ErrorAction SilentlyContinue
            if ($setupFiles.Count -gt 0) {
                $setupPath = $setupFiles[0].FullName
                Write-Host "Found setup.exe at: $setupPath"
            } else {
                Write-Host "ERROR: Could not find any installer in the downloaded media at $mediaPath"
                Get-ChildItem -Path $mediaPath -Recurse | ForEach-Object { Write-Host $_.FullName }
                Stop-Transcript
                exit 1
            }
        }
    }
} else {
    # For full installer, use it directly
    $setupPath = $installerPath
    Write-Host "Using full installer directly: $setupPath"
}

# Step 2: Run the installation
Write-Host "Starting SQL Server Express installation..."

# First check if it's a bootstrapper (SQLEXPR_x64_ENU.exe) which needs special treatment
$isBootstrapper = $setupPath -like "*SQLEXPR*.exe"
if ($isBootstrapper) {
    Write-Host "Detected SQL Express bootstrapper installer. Using appropriate install parameters."

    # First try to extract the installer
    Write-Host "Extracting SQL Express installer..."
    $extractArgs = "/x:`"$extractPath`" /q"
    Write-Host "Extraction command: $setupPath $extractArgs"

    $extractProcess = Start-Process -FilePath $setupPath -ArgumentList $extractArgs -Wait -PassThru -NoNewWindow
    $extractExitCode = $extractProcess.ExitCode

    Write-Host "Extraction completed with exit code: $extractExitCode"

    # Look for setup.exe in extracted files
    $setupFiles = Get-ChildItem -Path $extractPath -Recurse -Filter "setup.exe" -ErrorAction SilentlyContinue
    if ($setupFiles.Count -gt 0) {
        $setupPath = $setupFiles[0].FullName
        Write-Host "Found setup.exe at: $setupPath"

        # Run the actual setup.exe with appropriate parameters
        $setupArgs = "/Q /IACCEPTSQLSERVERLICENSETERMS /ACTION=Install /FEATURES=SQLEngine /INSTANCENAME=SQLEXPRESS /SQLSYSADMINACCOUNTS=`"BUILTIN\Administrators`" /TCPENABLED=1"
        Write-Host "Running extracted setup: $setupPath $setupArgs"

        $setupProcess = Start-Process -FilePath $setupPath -ArgumentList $setupArgs -Wait -PassThru -NoNewWindow
        $setupExitCode = $setupProcess.ExitCode

        Write-Host "Setup completed with exit code: $setupExitCode"

        # Check if installation succeeded
        $service = Get-Service -Name "MSSQL`$SQLEXPRESS" -ErrorAction SilentlyContinue
        if ($service) {
            Write-Host "SUCCESS: SQL Server Express service is now installed using extracted setup"
            Start-Service -Name "MSSQL`$SQLEXPRESS" -ErrorAction SilentlyContinue
            Stop-Transcript
            exit 0
        } else {
            Write-Host "Installation with extracted setup failed. Trying direct bootstrapper install."
        }
    } else {
        Write-Host "Could not find setup.exe in extracted files. Trying direct bootstrapper install."
    }

    # If extraction didn't work or setup.exe failed, try direct install
    $bootstrapArgs = "/Q /IACCEPTSQLSERVERLICENSETERMS /ACTION=Install /INSTANCENAME=SQLEXPRESS"
    Write-Host "Running bootstrapper with direct install: $setupPath $bootstrapArgs"

    $bootstrapProcess = Start-Process -FilePath $setupPath -ArgumentList $bootstrapArgs -Wait -PassThru -NoNewWindow
    $bootstrapExitCode = $bootstrapProcess.ExitCode

    Write-Host "Bootstrapper installation completed with exit code: $bootstrapExitCode"

} else {
    # Standard approach for regular setup.exe
    Write-Host "Using standard setup.exe installation approach."

    # Configuration INI file approach first if available
    $configPath = "c:\setup\mssql\sql_conf.ini"
    if (Test-Path $configPath) {
        Write-Host "Found configuration file. Attempting installation with config file."
        $configArgs = "/ConfigurationFile=`"$configPath`" /IACCEPTSQLSERVERLICENSETERMS /QUIET"

        $configInstallProcess = Start-Process -FilePath $setupPath -ArgumentList $configArgs -Wait -PassThru -NoNewWindow
        $configExitCode = $configInstallProcess.ExitCode

        Write-Host "Configuration-based installation completed with exit code: $configExitCode"

        # Check if installation succeeded
        $service = Get-Service -Name "MSSQL`$SQLEXPRESS" -ErrorAction SilentlyContinue
        if ($configExitCode -eq 0 -and $service) {
            Write-Host "SUCCESS: SQL Server Express installation completed successfully using configuration file"
            Stop-Transcript
            exit 0
        } else {
            Write-Host "Configuration-based installation failed or service not found. Trying command-line parameters."
        }
    }

    # Standard command-line installation approach
    $installArgs = "/Q /IACCEPTSQLSERVERLICENSETERMS /ACTION=Install /FEATURES=SQLEngine /INSTANCENAME=SQLEXPRESS /SQLSYSADMINACCOUNTS=`"BUILTIN\Administrators`" /TCPENABLED=1"
    Write-Host "Running installation with command-line parameters: $setupPath $installArgs"

    $installProcess = Start-Process -FilePath $setupPath -ArgumentList $installArgs -Wait -PassThru -NoNewWindow
    $installExitCode = $installProcess.ExitCode

    Write-Host "Command-line installation completed with exit code: $installExitCode"

    # Check if installation succeeded
    $service = Get-Service -Name "MSSQL`$SQLEXPRESS" -ErrorAction SilentlyContinue
    if ($installExitCode -eq 0 -and $service) {
        Write-Host "SUCCESS: SQL Server Express installation completed successfully using command-line parameters"
        Stop-Transcript
        exit 0
    }

    # If we get here, try one last approach with minimal parameters
    Write-Host "Previous installation attempts failed. Trying with minimal parameters..."
    $minimalArgs = "/Q /IACCEPTSQLSERVERLICENSETERMS /ACTION=Install /INSTANCENAME=SQLEXPRESS"
    Write-Host "Running installation with minimal parameters: $setupPath $minimalArgs"

    $minimalProcess = Start-Process -FilePath $setupPath -ArgumentList $minimalArgs -Wait -PassThru -NoNewWindow
    $minimalExitCode = $minimalProcess.ExitCode

    Write-Host "Minimal parameter installation completed with exit code: $minimalExitCode"
}

# Final check if installation succeeded
$service = Get-Service -Name "MSSQL`$SQLEXPRESS" -ErrorAction SilentlyContinue
if ($service) {
    Write-Host "SUCCESS: SQL Server Express service is now installed"

    # Ensure TCP/IP is enabled through registry
    try {
        # For SQL 2019
        $regPath = "HKLM:\Software\Microsoft\Microsoft SQL Server\MSSQL15.SQLEXPRESS\MSSQLServer\SuperSocketNetLib\Tcp\IPAll"
        if (Test-Path $regPath) {
            Write-Host "Setting TCP port in registry for SQL Server 2019"
            New-ItemProperty -Path $regPath -Name "TcpPort" -Value "1433" -PropertyType String -Force | Out-Null
        } else {
            # For SQL 2022
            $regPath = "HKLM:\Software\Microsoft\Microsoft SQL Server\MSSQL16.SQLEXPRESS\MSSQLServer\SuperSocketNetLib\Tcp\IPAll"
            if (Test-Path $regPath) {
                Write-Host "Setting TCP port in registry for SQL Server 2022"
                New-ItemProperty -Path $regPath -Name "TcpPort" -Value "1433" -PropertyType String -Force | Out-Null
            } else {
                # Try to find the correct registry path
                Write-Host "Looking for the correct registry path for SQL Server..."
                $sqlRegistryPaths = Get-ChildItem -Path "HKLM:\Software\Microsoft\Microsoft SQL Server" -ErrorAction SilentlyContinue
                foreach ($path in $sqlRegistryPaths) {
                    Write-Host "Found SQL Server registry path: $($path.Name)"
                }
            }
        }
    } catch {
        Write-Host "Warning: Failed to set TCP port in registry: $_"
    }

    # Start the service
    try {
        Start-Service -Name "MSSQL`$SQLEXPRESS" -ErrorAction SilentlyContinue
        Write-Host "SQL Server Express service started successfully"
    } catch {
        Write-Host "Warning: Failed to start SQL Server service: $_"
    }

    Stop-Transcript
    exit 0
} else {
    Write-Host "ERROR: All installation attempts failed. SQL Server Express service is not installed."

    # List installed services for debugging
    Write-Host "Listing all services containing 'SQL':"
    Get-Service | Where-Object { $_.DisplayName -like "*SQL*" } | ForEach-Object {
        Write-Host "Service Name: $($_.Name), Display Name: $($_.DisplayName), Status: $($_.Status)"
    }

    # Check if any SQL Server files were installed
    Write-Host "Checking for SQL Server installation directories:"
    $sqlPaths = @(
        "C:\Program Files\Microsoft SQL Server",
        "C:\Program Files (x86)\Microsoft SQL Server"
    )
    foreach ($path in $sqlPaths) {
        if (Test-Path $path) {
            Get-ChildItem -Path $path -Recurse -Depth 2 -ErrorAction SilentlyContinue |
                Where-Object { $_.PSIsContainer } |
                Select-Object FullName |
                ForEach-Object { Write-Host $_.FullName }
        }
    }

    Stop-Transcript
    exit 1
}
