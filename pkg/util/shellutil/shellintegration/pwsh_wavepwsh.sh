# We source this file with -NoExit -File
$env:PATH = {{.WSHBINDIR_PWSH}} + "{{.PATHSEP}}" + $env:PATH

# Source dynamic script from wsh token
$waveterm_swaptoken_output = wsh token $env:WAVETERM_SWAPTOKEN pwsh 2>$null | Out-String
if ($waveterm_swaptoken_output -and $waveterm_swaptoken_output -ne "") {
    Invoke-Expression $waveterm_swaptoken_output
}
Remove-Variable -Name waveterm_swaptoken_output
Remove-Item Env:WAVETERM_SWAPTOKEN

# Load Wave completions
wsh completion powershell | Out-String | Invoke-Expression

if ($PSVersionTable.PSVersion.Major -lt 7) {
    return  # skip OSC setup entirely
}

if ($PSStyle.FileInfo.Directory -eq "`e[44;1m") {
    $PSStyle.FileInfo.Directory = "`e[34;1m"
}

$Global:_WAVETERM_SI_FIRSTPROMPT = $true

# Command lifecycle for the tab activity indicator: emit OSC 16162 C on command start
# and D/A on completion (the "working" spinner / "done" badge is driven by these).
# PowerShell has no native preexec hook, so we emit C from PSReadLine's
# AddToHistoryHandler — it fires when a complete command line is accepted, right before
# execution, WITHOUT overriding Enter, so multiline editing and any user-defined key
# handlers keep working. Any existing AddToHistoryHandler is chained, not clobbered.
$Global:_WAVETERM_SI_INTEGRATION = $false
if (Get-Command Set-PSReadLineOption -ErrorAction SilentlyContinue) {
    try {
        $_waveterm_prev_add_to_history = (Get-PSReadLineOption).AddToHistoryHandler
        Set-PSReadLineOption -AddToHistoryHandler ({
            param([string]$line)
            $blocked = $false
            try { $blocked = [bool](_waveterm_si_blocked) } catch {}
            if (-not $blocked -and $line -and $line.Trim().Length -gt 0) {
                $cmd_bytes = [System.Text.Encoding]::UTF8.GetBytes($line)
                if ($cmd_bytes.Length -le 8192) {
                    $cmd64 = [System.Convert]::ToBase64String($cmd_bytes)
                    [Console]::Write("`e]16162;C;{`"cmd64`":`"$cmd64`"}`a")
                } else {
                    [Console]::Write("`e]16162;C`a")
                }
            }
            if ($null -ne $_waveterm_prev_add_to_history) {
                # the getter hands back a Func[string,object], so call it via .Invoke
                return $_waveterm_prev_add_to_history.Invoke($line)
            }
            return $true
        }).GetNewClosure()
        $Global:_WAVETERM_SI_INTEGRATION = $true
    } catch {
        $Global:_WAVETERM_SI_INTEGRATION = $false
    }
}

# shell integration
function Global:_waveterm_si_blocked {
    # Check if we're in tmux or screen
    return ($env:TMUX -or $env:STY -or $env:TERM -like "tmux*" -or $env:TERM -like "screen*")
}

function Global:_waveterm_si_osc7 {
    if (_waveterm_si_blocked) { return }

    # Percent-encode the raw path as-is (handles UNC, drive letters, etc.)
    $encoded_pwd = [System.Uri]::EscapeDataString($PWD.Path)

    # OSC 7 - current directory
    Write-Host -NoNewline "`e]7;file://localhost/$encoded_pwd`a"
}

function Global:_waveterm_si_prompt {
    # Capture the previous command's status FIRST — any command below resets $?.
    $_waveterm_ok = $?
    $_waveterm_lastexit = $LASTEXITCODE
    if (_waveterm_si_blocked) { return }

    if ($Global:_WAVETERM_SI_FIRSTPROMPT) {
        # not sending uname
        $shellversion = $PSVersionTable.PSVersion.ToString()
        $integration = if ($Global:_WAVETERM_SI_INTEGRATION) { 'true' } else { 'false' }
        Write-Host -NoNewline "`e]16162;M;{`"shell`":`"pwsh`",`"shellversion`":`"$shellversion`",`"integration`":$integration}`a"
        $Global:_WAVETERM_SI_FIRSTPROMPT = $false
    } else {
        # D — previous command finished; mirror the bash integration's exit-code mapping
        $exitcode = if ($_waveterm_ok) { 0 } elseif ($null -ne $_waveterm_lastexit) { $_waveterm_lastexit } else { 1 }
        Write-Host -NoNewline "`e]16162;D;{`"exitcode`":$exitcode}`a"
    }

    _waveterm_si_osc7
    Write-Host -NoNewline "`e]16162;A`a"
}

# Add the OSC 7 call to the prompt function
if (Test-Path Function:\prompt) {
    $global:_waveterm_original_prompt = $function:prompt
    function Global:prompt {
        _waveterm_si_prompt
        & $global:_waveterm_original_prompt
    }
} else {
    function Global:prompt {
        _waveterm_si_prompt
        "PS $($executionContext.SessionState.Path.CurrentLocation)$('>' * ($nestedPromptLevel + 1)) "
    }
}
