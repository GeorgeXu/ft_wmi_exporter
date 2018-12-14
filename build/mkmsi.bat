@echo off
setlocal

set version=1.0.0

candle.exe -ext WixFirewallExtension -ext WixUtilExtension -dVersion=%version%  setup.wxs

light.exe -spdb -ext WixFirewallExtension -ext WixUtilExtension -sice:ICE20 setup.wixobj

endlocal