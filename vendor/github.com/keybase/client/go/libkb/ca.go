// Copyright 2015 Keybase, Inc. All rights reserved. Use of
// this source code is governed by the included BSD license.

package libkb

var BundledCAs = map[string]string{
	"api.keybase.io":          apiCA,
	"gregord.kbfs.keybase.io": KBFSProdCA,
	"gregord.dev.keybase.io":  KBFSDevCA,
}

const apiCA = `-----BEGIN CERTIFICATE-----
MIIGmzCCBIOgAwIBAgIJAPzhpcIBaOeNMA0GCSqGSIb3DQEBBQUAMIGPMQswCQYD
VQQGEwJVUzELMAkGA1UECBMCTlkxETAPBgNVBAcTCE5ldyBZb3JrMRQwEgYDVQQK
EwtLZXliYXNlIExMQzEXMBUGA1UECxMOQ2VydCBBdXRob3JpdHkxEzARBgNVBAMT
CmtleWJhc2UuaW8xHDAaBgkqhkiG9w0BCQEWDWNhQGtleWJhc2UuaW8wHhcNMTQw
MTAyMTY0MjMzWhcNMjMxMjMxMTY0MjMzWjCBjzELMAkGA1UEBhMCVVMxCzAJBgNV
BAgTAk5ZMREwDwYDVQQHEwhOZXcgWW9yazEUMBIGA1UEChMLS2V5YmFzZSBMTEMx
FzAVBgNVBAsTDkNlcnQgQXV0aG9yaXR5MRMwEQYDVQQDEwprZXliYXNlLmlvMRww
GgYJKoZIhvcNAQkBFg1jYUBrZXliYXNlLmlvMIICIjANBgkqhkiG9w0BAQEFAAOC
Ag8AMIICCgKCAgEA3sLA6ZG8uOvmlFvFLVIOURmcQrZyMFKbVu9/TeDiemls3w3/
JzVTduD+7KiUi9R7QcCW/V1ZpReTfunm7rfACiJ1fpIkjSQrgsvKDLghIzxIS5FM
I8utet5p6QtuJhaAwmmXn8xX05FvqWNbrcXRdpL4goFdigPsFK2xhTUiWatLMste
oShI7+zmrgkx75LeLMD0bL2uOf87JjOzbY8x2sUIZLGwPoATyG8WS38ey6KkJxRj
AhG3p+OTYEjYSrsAtQA6ImbeDpfSHKOB8HF3nVp//Eb4HEiEsWwBRbQXvAWh3DYL
GukFW0wiO0HVCoWY+bHL/Mqa0NdRGOlLsbL4Z4pLrhqKgSDU8umX9YuNRRaB0P5n
TkzyU6axHqzq990Gep/I62bjsBdYYp+DjSPK43mXRrfWJl2NTcl8xKAyfsOW+9hQ
9vwK0tpSicNxfYuUZs0BhfjSZ/Tc6Z1ERdgUYRiXTtohl+SRA2IgZMloHCllVMNj
EjXhguvHgLAOrcuyhVBupiUQGUHQvkMsr1Uz8VPNDFOJedwucRU2AaR881bknnSb
ds9+zNLsvUFV+BK7Qdnt/WkFpYL78rGwY47msi9Ooddx6fPyeg3qkJGM6cwn/boy
w9lQeleYDq8kyJdixIAxtAskNzRPJ4nDu2izTfByQoM8epwAWboc/gNFObMCAwEA
AaOB9zCB9DAdBgNVHQ4EFgQURqpATOw1gVVrzlqqFKbkfaKXvwowgcQGA1UdIwSB
vDCBuYAURqpATOw1gVVrzlqqFKbkfaKXvwqhgZWkgZIwgY8xCzAJBgNVBAYTAlVT
MQswCQYDVQQIEwJOWTERMA8GA1UEBxMITmV3IFlvcmsxFDASBgNVBAoTC0tleWJh
c2UgTExDMRcwFQYDVQQLEw5DZXJ0IEF1dGhvcml0eTETMBEGA1UEAxMKa2V5YmFz
ZS5pbzEcMBoGCSqGSIb3DQEJARYNY2FAa2V5YmFzZS5pb4IJAPzhpcIBaOeNMAwG
A1UdEwQFMAMBAf8wDQYJKoZIhvcNAQEFBQADggIBAA3Z5FIhulYghMuHdcHYTYWc
7xT5WD4hXQ0WALZs4p5Y+b2Af54o6v1wUE1Au97FORq5CsFXX/kGl/JzzTimeucn
YJwGuXMpilrlHCBAL5/lSQjA7qbYIolQ3SB9ON+LYuF1jKB9k8SqNp7qzucxT3tO
b8ZMDEPNsseC7NE2uwNtcW3yrTh6WZnSqg/jwswiWjHYDdG7U8FjMYlRol3wPux2
PizGbSgiR+ztI2OthxtxNWMrT9XKxNQTpcxOXnLuhiSwqH8PoY17ecP8VPpaa0K6
zym0zSkbroqydazaxcXRk3eSlc02Ktk7HzRzuqQQXhRMkxVnHbFHgGsz03L533pm
mlIEgBMggZkHwNvs1LR7f3v2McdKulDH7Mv8yyfguuQ5Jxxt7RJhUuqSudbEhoaM
6jAJwBkMFxsV2YnyFEd3eZ/qBYPf7TYHhyzmHW6WkSypGqSnXd4gYpJ8o7LxSf4F
inLjxRD+H9Xn1UVXWLM0gaBB7zZcXd2zjMpRsWgezf5IR5vyakJsc7fxzgor3Qeq
Ri6LvdEkhhFVl5rHMQBwNOPngySrq8cs/ikTLTfQVTYXXA4Ba1YyiMOlfaR1LhKw
If1AkUV0tfCTNRZ01EotKSK77+o+k214n+BAu+7mO+9B5Kb7lMFQcuWCHXKYB2Md
cT7Yh09F0QpFUd0ymEfv
-----END CERTIFICATE-----`

const KBFSProdCA = `
-----BEGIN CERTIFICATE-----
MIIDjzCCAnegAwIBAgIQII2p7AsMIZbRiknQbMjdbTANBgkqhkiG9w0BAQsFADBR
MRIwEAYKCZImiZPyLGQBGRYCaW8xHDAaBgoJkiaJk/IsZAEZFgxrYmZzLmtleWJh
c2UxHTAbBgNVBAMMFEtleWJhc2UgS0JGUyBDQSBwcm9kMB4XDTE3MDkyNTIzMTcy
MFoXDTIyMDkyNDIzMTcyMFowUTESMBAGCgmSJomT8ixkARkWAmlvMRwwGgYKCZIm
iZPyLGQBGRYMa2Jmcy5rZXliYXNlMR0wGwYDVQQDDBRLZXliYXNlIEtCRlMgQ0Eg
cHJvZDCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBAM1iLrzo/lCOHS9H
mJnpnnWyNqmuDeRGwZwmZMRnlOVRz2Lx3MPkg63DaNfiNfP9dZtn8oCAmlP5zh03
fFk2cvFgBIDFmE8DlBS47NTQlWlNHgEMam6FqZdVLuFMuJeLdz6DqpCi+5s8Mild
Q3SY2IcK8wbk1vegDuYSvk28WIh/mHAjxuFvjcGklAA58jmnyV9GMtTfJTA0Vfrp
ina2belNMZV7zJWz6x7Sa/NJbtGzQ9MYVJaUpiQxUtSllAlG/iBFftuUZOtBR1h3
3B43dK+JVJgbMBsoNlh0LWeMjtj6NTys4uhBZi40Bv8dBSbfql97sBDlHK+WDrrF
ky72elcCAwEAAaNjMGEwDwYDVR0TAQH/BAUwAwEB/zAOBgNVHQ8BAf8EBAMCAQYw
HQYDVR0OBBYEFC6A4MGXsSijzz4mgEr+ElUVi6jLMB8GA1UdIwQYMBaAFC6A4MGX
sSijzz4mgEr+ElUVi6jLMA0GCSqGSIb3DQEBCwUAA4IBAQA4DGYdfETrg96rZFBC
RZuFLSpBnxbGcCA1kRi/EvDLGCpCcrQ1NwoW5/A6+gWWX4NUVSPQj7nY9T9EtGuC
eQ+5UXG36RwE+nsiGhjGqyd747ClSEIAslxT7XbKDjJM8o1fnuylz58duqoLNfah
XA9WgttmJcbGjGIodHgkTlx4TWLqvL8BAZ4Dxm7nauBNTKjpIy7PHnMZS8ROP5S1
baG12aXpANJRwbp1/5W5QAL2JFRDn0aakNA3d9R+cgZYwObAM6+uOKxhRkh9rW+O
NGYdmQnMFi8+aX8XhGsK8RlnQBji7Bo39iC7oesK+DoZiP/M8yUQAyoDzTQnalkL
eMyM
-----END CERTIFICATE-----
` /* created on 9/25/2017; expires on 9/24/2022 */ +
	`-----BEGIN CERTIFICATE-----
MIIDkDCCAnigAwIBAgIRAL1MQ3C37AuGO8gFqfhqb9EwDQYJKoZIhvcNAQELBQAw
UTESMBAGCgmSJomT8ixkARkWAmlvMRwwGgYKCZImiZPyLGQBGRYMa2Jmcy5rZXli
YXNlMR0wGwYDVQQDDBRLZXliYXNlIEtCRlMgQ0EgcHJvZDAeFw0xNTExMDgyMzM3
MDFaFw0xNzExMDcyMzM3MDFaMFExEjAQBgoJkiaJk/IsZAEZFgJpbzEcMBoGCgmS
JomT8ixkARkWDGtiZnMua2V5YmFzZTEdMBsGA1UEAwwUS2V5YmFzZSBLQkZTIENB
IHByb2QwggEiMA0GCSqGSIb3DQEBAQUAA4IBDwAwggEKAoIBAQCr9ttTzL093jPt
WstzWR19qvLprd778ALqShZYZughuXPULgOck4AQW27vlp1nY8+7sBnWgstzL6Gv
dTQU61e34yOeAFYyKoWPFHyeo/g1y+LANgLdLbeOatOlWyM2sb/f0K3SKpusp/9J
0ylpDyko97MAI28spwX1d7L/qlDV6ryce4GrzElp3J8j3TZ3cju5rEldn8BSnLYw
i/2/Sc93GwhkjI03MZvuWaJQXQjTMALVzx5gFzshUymV4yrJfQbmBTwODf1yucsQ
NrWDiKWcFXe5dR8BWBZG7lslZeGYaHQ6lc3TgGwaPobpaZpzVEt3Crb9HAuTVl8/
Ynlw2XvzAgMBAAGjYzBhMA8GA1UdEwEB/wQFMAMBAf8wDgYDVR0PAQH/BAQDAgEG
MB0GA1UdDgQWBBSg9AYko8IqCwg2awZOZO6TW+ITsjAfBgNVHSMEGDAWgBSg9AYk
o8IqCwg2awZOZO6TW+ITsjANBgkqhkiG9w0BAQsFAAOCAQEAAJ2oOlY+DCDWr73m
TrR3Kfx+bDzvU1IZviKKooGGPjG+apcz5rWoKhjkO593ORCrygAvITnAI4v2Eaic
h2zYfWkOnCI2YYvVChR0TSJfa2+gxZFUqxRb68zMgcTxGZTZUonEX4nCJjkrSx3M
ATZkFWJDPPVci6o87VbpnKOc3mep3i1s3Cvw0GMHP+yVgw8Y0BpXII5hGbCODmoh
d2mdg2gjlOVBCfTEAe7cgUx9/lraQwUurUjDO3g54NZo/pcoc9koIW+Ai+saF5gA
UnFkqAOuEw0y4Fxzr9pw9naKF3KMlEJf6CiDJ4xspNzPZFupuepKitRrlrzofYuW
OXgZAw==
-----END CERTIFICATE-----
` /* expires on 11/7/2017 */

const KBFSDevCA = `
-----BEGIN CERTIFICATE-----
MIIDjDCCAnSgAwIBAgIRANHrLgSTwwi2RIR16ccqNQ8wDQYJKoZIhvcNAQELBQAw
TzESMBAGCgmSJomT8ixkARkWAmlvMRswGQYKCZImiZPyLGQBGRYLZGV2LmtleWJh
c2UxHDAaBgNVBAMME0tleWJhc2UgS0JGUyBDQSBkZXYwHhcNMTcwOTI1MjMxNzAw
WhcNMjIwOTI0MjMxNzAwWjBPMRIwEAYKCZImiZPyLGQBGRYCaW8xGzAZBgoJkiaJ
k/IsZAEZFgtkZXYua2V5YmFzZTEcMBoGA1UEAwwTS2V5YmFzZSBLQkZTIENBIGRl
djCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBAOVD9fUTBH+93HdKFb2C
btck0zwxV7FLiAGi0wA0UGKOUva8Crb0+iephZUyjCS/AKiiE6sm5+FWfORPtDkn
+rr6YaNuOtYcdgyb+ggWbE95XAp1yVgKDbASnzJkXPAfgtGmQs/9w6LHkylPD+Mk
5cA/lgqSEFbX1hKQpzabFKCJsQG7LxkWpaaK49l5nzMTIad9xrAtImO+tfndLxVC
pXWDROogku6H0wKLRDBQmG1INN59IRbL3H1F8AvYSP0moyfqVx+uI1ShIpOHK/G4
JitvQ7pVtBpN300+idAqba5pJ7gK3Ggxyj5jrz1IUkAn2KPemxZmcRn3QvXWhBEa
pJ0CAwEAAaNjMGEwDwYDVR0TAQH/BAUwAwEB/zAOBgNVHQ8BAf8EBAMCAQYwHQYD
VR0OBBYEFElkKoofwwezv+CflHySNBQ9tq8gMB8GA1UdIwQYMBaAFElkKoofwwez
v+CflHySNBQ9tq8gMA0GCSqGSIb3DQEBCwUAA4IBAQBWreC17W2iQwsuBOcBNtg9
XfmsokA835V9axomY/KHOOlK6DMtA9/HV3Ycntm+1nlKV4C5J2qmvA4wie7K+woS
Z0+BOnIBSa7vYBfWGwSzJYo397NP9fe51rs5SqlvMXdEgKYZUWnPTBcjjxr+WvES
v4OUHgJhyH8nMjockI5Ge6qIj8hicBZPI48k0fGeFXNSTyK4szXotOMcI+J7ygIi
4jkjVSRDPTnc7f7h0zrghVMOVCWvIXN4Yj4SOEJKhYiEoQ4ouJIiR3j+gOfHVB0Y
rlzSOc4hBMLqxPq7/cqx9t+vhp9PefkiHQdmnHtt8mqE0X0RU1XEznttaOeTUQE3
-----END CERTIFICATE-----
` /* created on 9/25/2017; expires on 9/24/2022 */ +
	`
-----BEGIN CERTIFICATE-----
MIIDjDCCAnSgAwIBAgIRANRMeoRz3Xg5c8kT2f5k9wMwDQYJKoZIhvcNAQELBQAw
TzESMBAGCgmSJomT8ixkARkWAmlvMRswGQYKCZImiZPyLGQBGRYLZGV2LmtleWJh
c2UxHDAaBgNVBAMME0tleWJhc2UgS0JGUyBDQSBkZXYwHhcNMTUwOTIzMTk0NzM3
WhcNMTcwOTIyMTk0NzM3WjBPMRIwEAYKCZImiZPyLGQBGRYCaW8xGzAZBgoJkiaJ
k/IsZAEZFgtkZXYua2V5YmFzZTEcMBoGA1UEAwwTS2V5YmFzZSBLQkZTIENBIGRl
djCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBALWCTpo8NNeGM6nIkNW+
4qiM8ocuFjDw2Er6XJhncgj7xjTGf/9yZqnXeGHyHGT66AtKl5bc8son4+npWmvs
47OXORF7YGi89d9KBlIC4NCetZLBSVWiSG+XXSKrmIffi6D0UojpZc2blnzgejEO
ii1uCDSaj6TRLcC8z/eXKq+DtPcfNnPL0pu5CiUNrH1cA9PS+jO1OonCGPG5yVjW
bBw0nQfThhapm9IohtdbYzlQiSbE1+3ctNwCPLas3mmUWkcrrVbn1Fa54LnfNR2u
pnZRNZ7czfB/vtymUJ6/y8dLYTmnzMFFYy416FOmvr4NqLBkaMWg9xp+KeR30044
AicCAwEAAaNjMGEwDwYDVR0TAQH/BAUwAwEB/zAOBgNVHQ8BAf8EBAMCAQYwHQYD
VR0OBBYEFBdb6+h+Qq5vXUWo99QbatQTX6u+MB8GA1UdIwQYMBaAFBdb6+h+Qq5v
XUWo99QbatQTX6u+MA0GCSqGSIb3DQEBCwUAA4IBAQArhp0KXfJHEhVcUXqYYjdn
pZQjq3+0aKjMjgnVWekxwwBARh4ycy2e7066ru1eDZr6myGYK+/vjXituWtq7/c/
Fifezgje6o9lB1TPamgQeE8slqqAgc3OxTqbAAf+rxJelcI6aOm7tqX04k8Aiuhm
dr64cM/NsZTKUbrCHCVNHPNj8wWkrb9pbXH/q0+Gt/gw4MiL6p1YuSr4SIENqDpP
VFiOCcbOSiw5OHPe/VwLts/g3e3NSXqd53nQW1/CgpSBdT73oWw+SBfv21KuJN5K
745S8d9JfbLItWgM73o94MSLOpUEl2F7qqXj2eOBEYWIMbRjMMZ7Vzmuo5wo3M8i
-----END CERTIFICATE-----
` /* expired on 9/22/2017 */
