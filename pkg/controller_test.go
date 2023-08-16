package pkg

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func Test_parseXmlCliPlainString(t *testing.T) {
	input := `Some string
with multiple
lines`

	assert.Equal(t, input, parseCliXml(input))
}

func Test_parseXmlCliErrorObject(t *testing.T) {
	input := `#< CLIXML
<Objs Version="1.1.0.1" xmlns="http://schemas.microsoft.com/powershell/2004/04">
  <S S="Error">New-VHD : Failed to create the virtual hard disk._x000D__x000A_</S>
  <S S="Error">The system failed to create 'V:\Hyper-V\Virtual Hard Disks\pvc-b0475d14-782d-4485-b09c-ee93150dca72.vhdx'._x000D__x000A_</S>
  <S S="Error">Failed to create the virtual hard disk._x000D__x000A_</S>
  <S S="Error">The system failed to create 'V:\Hyper-V\Virtual Hard Disks\pvc-b0475d14-782d-4485-b09c-ee93150dca72.vhdx': The file _x000D__x000A_</S>
  <S S="Error">exists. (0x80070050)._x000D__x000A_</S>
  <S S="Error">At line:1 char:42_x000D__x000A_</S>
  <S S="Error">+ ... lyContinue';New-VHD -Path 'V:\Hyper-V\Virtual Hard Disks\pvc-b0475d14 ..._x000D__x000A_</S>
  <S S="Error">+                 ~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~_x000D__x000A_</S>
  <S S="Error">    + CategoryInfo          : NotSpecified: (:) [New-VHD], VirtualizationException_x000D__x000A_</S>
  <S S="Error">    + FullyQualifiedErrorId : OperationFailed,Microsoft.Vhd.PowerShell.Cmdlets.NewVhd_x000D__x000A_</S>
  <S S="Error"> _x000D__x000A_</S>
</Objs> 
`

	output := `New-VHD : Failed to create the virtual hard disk.
The system failed to create 'V:\Hyper-V\Virtual Hard Disks\pvc-b0475d14-782d-4485-b09c-ee93150dca72.vhdx'.
Failed to create the virtual hard disk.
The system failed to create 'V:\Hyper-V\Virtual Hard Disks\pvc-b0475d14-782d-4485-b09c-ee93150dca72.vhdx': The file 
exists. (0x80070050).
At line:1 char:42
+ ... lyContinue';New-VHD -Path 'V:\Hyper-V\Virtual Hard Disks\pvc-b0475d14 ...
+                 ~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
    + CategoryInfo          : NotSpecified: (:) [New-VHD], VirtualizationException
    + FullyQualifiedErrorId : OperationFailed,Microsoft.Vhd.PowerShell.Cmdlets.NewVhd`

	assert.Equal(t, output, parseCliXml(input))
}

func Test_parseXmlCliUtf16SurrogatePair(t *testing.T) {
	// I think this is right... https://stackoverflow.com/questions/38147259/how-can-i-convert-surrogate-pairs-to-normal-string-in-python
	input := "#< CLIXML\r\n<Objs><S>_x0001f64f_</S></Objs>"
	output := "🙏"
	assert.Equal(t, output, parseCliXml(input))
}

func Test_parseXmlCliTrailingCharacters(t *testing.T) {
	input := "#< CLIXML\r\n<Objs><S>_x000D__x000A_Some string</S></Objs>"
	output := "Some string"
	assert.Equal(t, output, parseCliXml(input))
}
