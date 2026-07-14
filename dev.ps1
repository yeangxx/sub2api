[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"

$repoRoot = $PSScriptRoot
$backendDir = Join-Path $repoRoot "backend"
$frontendDir = Join-Path $repoRoot "frontend"
$deployDir = Join-Path $repoRoot "deploy"
$composeBase = Join-Path $deployDir "docker-compose.local.yml"
$composeDev = Join-Path $deployDir "docker-compose.host-dev.yml"
$backendConfig = Join-Path $backendDir "config.yaml"
$frontendConfig = Join-Path $frontendDir ".env.local"

function Resolve-RequiredCommand {
    param([Parameter(Mandatory)][string]$Name)

    $command = Get-Command $Name -ErrorAction SilentlyContinue
    if (-not $command) {
        throw "Required command '$Name' was not found in PATH."
    }
    return $command.Source
}

function Assert-RequiredFile {
    param([Parameter(Mandatory)][string]$Path)

    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        throw "Required local configuration file is missing: $Path"
    }
}

function Assert-PortAvailable {
    param(
        [Parameter(Mandatory)][int]$Port,
        [Parameter(Mandatory)][string]$Service
    )

    $listener = Get-NetTCPConnection -State Listen -LocalPort $Port -ErrorAction SilentlyContinue
    if ($listener) {
        $owners = ($listener | Select-Object -ExpandProperty OwningProcess -Unique) -join ", "
        throw "$Service port $Port is already in use (PID: $owners). Stop that process and run dev.ps1 again."
    }
}

function Stop-ProcessTree {
    param([Parameter(Mandatory)][int]$RootProcessId)

    $children = @(
        Get-CimInstance Win32_Process -Filter "ParentProcessId = $RootProcessId" -ErrorAction SilentlyContinue
    )
    foreach ($child in $children) {
        Stop-ProcessTree -RootProcessId ([int]$child.ProcessId)
    }
    Stop-Process -Id $RootProcessId -Force -ErrorAction SilentlyContinue
}

$backendProcess = $null
$frontendProcess = $null
$exitCode = 0

try {
    $dockerCommand = Resolve-RequiredCommand "docker.exe"
    $goCommand = Resolve-RequiredCommand "go.exe"
    $pnpmCommand = Resolve-RequiredCommand "pnpm.cmd"

    Assert-RequiredFile $composeBase
    Assert-RequiredFile $composeDev
    Assert-RequiredFile $backendConfig
    Assert-RequiredFile $frontendConfig

    Write-Host "[dev] Starting PostgreSQL and Redis in Docker..." -ForegroundColor Cyan
    Push-Location $deployDir
    try {
        & $dockerCommand compose -f $composeBase -f $composeDev up -d postgres redis
        if ($LASTEXITCODE -ne 0) {
            throw "Docker Compose failed with exit code $LASTEXITCODE."
        }
    }
    finally {
        Pop-Location
    }

    Assert-PortAvailable -Port 8080 -Service "Backend"
    Assert-PortAvailable -Port 3001 -Service "Frontend"

    Write-Host "[dev] Backend:  http://127.0.0.1:8080" -ForegroundColor Green
    Write-Host "[dev] Frontend: http://127.0.0.1:3001" -ForegroundColor Green
    Write-Host "[dev] Press Ctrl+C to stop frontend and backend. Docker dependencies will keep running." -ForegroundColor Yellow

    $backendProcess = Start-Process `
        -FilePath $goCommand `
        -ArgumentList @("run", "./cmd/server") `
        -WorkingDirectory $backendDir `
        -NoNewWindow `
        -PassThru

    $frontendProcess = Start-Process `
        -FilePath $pnpmCommand `
        -ArgumentList @("dev") `
        -WorkingDirectory $frontendDir `
        -NoNewWindow `
        -PassThru

    while ($true) {
        Start-Sleep -Seconds 1
        $backendProcess.Refresh()
        $frontendProcess.Refresh()

        if ($backendProcess.HasExited) {
            throw "Backend exited unexpectedly with code $($backendProcess.ExitCode)."
        }
        if ($frontendProcess.HasExited) {
            throw "Frontend exited unexpectedly with code $($frontendProcess.ExitCode)."
        }
    }
}
catch {
    [Console]::Error.WriteLine("[dev] ERROR: $($_.Exception.Message)")
    $exitCode = 1
}
finally {
    Write-Host "`n[dev] Stopping frontend and backend..." -ForegroundColor Yellow
    if ($frontendProcess -and -not $frontendProcess.HasExited) {
        Stop-ProcessTree -RootProcessId $frontendProcess.Id
    }
    if ($backendProcess -and -not $backendProcess.HasExited) {
        Stop-ProcessTree -RootProcessId $backendProcess.Id
    }
}

exit $exitCode
