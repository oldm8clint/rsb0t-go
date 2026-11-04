package archive

import (
	"fmt"
	"io"
	"net/http"
)

// FetchComponents fetches and parses the interface archive straight off a running
// engine's HTTP endpoint: reads /crc0 to find the interface archive's current crc (index
// 3 of the four u32s that endpoint returns — the same convention every caller below
// already used), fetches /interface<crc>, and parses its "data" entry via
// ParseInterfaces.
//
// This is the shared implementation of what five separate cmd/*test tools
// (cmd/banktest, cmd/equiptest, cmd/fishtest, cmd/muletest, cmd/shoptest) each carried as
// their own copy-pasted fetchInterfaceComponents/httpGet pair — kept here so a non-test
// caller that isn't a standalone tool (internal/fleet, wiring a bot.Context's Components
// field for cmd/fleet) has one implementation to call instead of a sixth copy. The
// cmd/*test tools themselves are free to keep calling this too; cmd/fishtest does, as of
// its own extraction into internal/scripts/fisherrunner.
func FetchComponents(httpBase string) (Components, error) {
	crc0, err := httpGetBytes(httpBase + "crc0")
	if err != nil {
		return nil, fmt.Errorf("archive: fetching /crc0: %w", err)
	}
	if len(crc0) < 4*4 {
		return nil, fmt.Errorf("archive: /crc0 response too short (%d bytes)", len(crc0))
	}
	interfaceCRC := int32(crc0[3*4])<<24 | int32(crc0[3*4+1])<<16 | int32(crc0[3*4+2])<<8 | int32(crc0[3*4+3])

	raw, err := httpGetBytes(fmt.Sprintf("%sinterface%d", httpBase, interfaceCRC))
	if err != nil {
		return nil, fmt.Errorf("archive: fetching interface archive: %w", err)
	}
	jag, err := ParseJagFile(raw)
	if err != nil {
		return nil, fmt.Errorf("archive: parsing interface jagfile: %w", err)
	}
	data, err := jag.Read("data")
	if err != nil {
		return nil, fmt.Errorf("archive: reading interface archive's 'data' entry: %w", err)
	}
	if data == nil {
		return nil, fmt.Errorf("archive: interface archive has no 'data' entry")
	}
	return ParseInterfaces(data), nil
}

func httpGetBytes(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}
