<#
.SYNOPSIS
Lanza Kaname y lista cada socket de su árbol de procesos. Falla si alguno escucha.

.DESCRIPTION
Es la mitad dinámica de «la app no abre ningún socket»: la estática la hace
`go test .` leyendo el código, y esta corre el binario de verdad —con el
WebView2 y sus procesos hijos incluidos— y le pregunta al sistema.

Lo que se espera ver: ningún TCP en estado Listen, ningún endpoint UDP, y
kaname.exe sin un solo socket hasta que alguien conecta a una base.

Lo que se ve además, y no es de la app: el proceso del navegador de WebView2
(msedgewebview2.exe) abre dos o tres HTTPS salientes a Microsoft al arrancar.
Es el runtime reportando y buscando configuración por su cuenta; el plan lo
tiene anotado como el precio de no embeber Chromium. El script lo lista para
que se vea, y no lo cuenta como fallo: lo que se mide acá es lo que Kaname
abre, y una conexión saliente de Edge no es un puerto escuchando.

.PARAMETER Binario
El ejecutable a lanzar. Tiene que ser el de producción: el build de debug abre
el puerto 9222 del inspector de Chromium, que es exactamente un socket
escuchando, y el script lo rechaza.

.PARAMETER Espera
Segundos entre lanzar y mirar. La ventana y los procesos de WebView2 tardan un
par de segundos en aparecer.

.EXAMPLE
wails3 task build
.\scripts\sockets.ps1
#>
param(
    [string]$Binario = "bin\kaname.exe",
    [int]$Espera = 8
)

$ErrorActionPreference = "Stop"

if (-not (Test-Path $Binario)) {
    throw "no existe $Binario. Construilo con: wails3 task build"
}
if (Select-String -Path $Binario -Pattern "remote-debugging-port" -Quiet) {
    throw "$Binario es un build de debug (lleva --remote-debugging-port): abre el 9222 y la medición no vale. Reconstruilo limpio."
}

# El árbol entero, no solo el proceso raíz: los sockets de WebView2 son de
# msedgewebview2.exe, que cuelga de kaname.exe.
function Get-Arbol([int]$Raiz) {
    $ids = @($Raiz)
    foreach ($hijo in Get-CimInstance Win32_Process -Filter "ParentProcessId = $Raiz") {
        $ids += Get-Arbol $hijo.ProcessId
    }
    return $ids
}

$proceso = Start-Process -FilePath (Resolve-Path $Binario) -PassThru
try {
    Start-Sleep -Seconds $Espera

    # Un binario que murió al arrancar —sin WebView2, un panic en main— no
    # tiene sockets, y eso no es un resultado: es que no se midió nada.
    if ($proceso.HasExited) {
        throw "$Binario terminó antes de la medición (exit $($proceso.ExitCode)): no hay nada que medir"
    }

    $pids = Get-Arbol $proceso.Id
    $procesos = Get-CimInstance Win32_Process | Where-Object { $pids -contains $_.ProcessId }
    if ($procesos.Count -lt 2) {
        throw "solo está $Binario, sin procesos de WebView2: la ventana no llegó a abrirse, subí -Espera"
    }
    Write-Output ("Procesos ({0}):" -f $procesos.Count)
    $procesos | ForEach-Object { Write-Output ("  {0,6}  {1}" -f $_.ProcessId, $_.Name) }

    $tcp = @(Get-NetTCPConnection -ErrorAction SilentlyContinue | Where-Object { $pids -contains $_.OwningProcess })
    $udp = @(Get-NetUDPEndpoint -ErrorAction SilentlyContinue | Where-Object { $pids -contains $_.OwningProcess })

    Write-Output ("TCP ({0}):" -f $tcp.Count)
    $tcp | ForEach-Object { Write-Output ("  {0,6}  {1}:{2} -> {3}:{4}  {5}" -f $_.OwningProcess, $_.LocalAddress, $_.LocalPort, $_.RemoteAddress, $_.RemotePort, $_.State) }
    Write-Output ("UDP ({0}):" -f $udp.Count)
    $udp | ForEach-Object { Write-Output ("  {0,6}  {1}:{2}" -f $_.OwningProcess, $_.LocalAddress, $_.LocalPort) }
}
finally {
    # Solo el proceso que lanzó este script. Una instancia que ya estuviera
    # abierta es de la persona y no se toca.
    Stop-Process -Id $proceso.Id -Force -ErrorAction SilentlyContinue
}

$escuchando = @($tcp | Where-Object { $_.State -eq "Listen" })
$deKaname = @(($tcp + $udp) | Where-Object { $_.OwningProcess -eq $proceso.Id })
if ($escuchando.Count -gt 0 -or $udp.Count -gt 0 -or $deKaname.Count -gt 0) {
    Write-Output "RESULTADO: hay un socket escuchando, un endpoint UDP o un socket de kaname.exe sin haber conectado a nada."
    exit 1
}
Write-Output "RESULTADO: ningún socket escuchando, ningún endpoint UDP, ningún socket de kaname.exe."
