package main

import (
	"flag"
	"log"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/danomagnum/gologix"
)

const (
	connSizeLargeDefault    = 4000   // default large connection size
	connSizeStandardDefault = 511    // default small connection size
	connSizeStandardMax     = 511    // maximum size of connection for standard
	portDefault             = 44818  // default CIP port
	vendorIdDefault         = 0x9999 // default vendor id. Used to prevent vendor ID conflicts
	socketTimeoutDefault    = time.Second * 10
	rpiDefault              = time.Millisecond * 2500
)

type ImplicitIn struct {
	Data  [9]byte
	Count byte
}

const (
	uint32TagName = "uint32"
)

type probeCase struct {
	instance int
	out      int
	in       int
}

func runOneSubscription(client *gologix.Client, listenAddr string, timeoutSeconds int, instance int, out int, in int) (bool, error) {
	sub, err := gologix.SubscribeImplicit[ImplicitIn](client, gologix.ImplicitSubscriptionConfig{
		RPI:              200 * time.Millisecond,
		TransportTrigger: 0xA3,
		AssemblyPath: &gologix.ImplicitAssemblyPathConfig{
			AssemblyClass:  0x04,
			ConfigInstance: gologix.CIPInstance(instance),
			OutputPoint:    gologix.CIPConnectionPoint(out),
			InputPoint:     gologix.CIPConnectionPoint(in),
		},
		ListenAddress: listenAddr,
		ValueBuffer:   64,
	})
	if err != nil {
		return false, err
	}
	defer func() {
		if stopErr := sub.Stop(); stopErr != nil {
			log.Printf("subscription stop warning: %v", stopErr)
		}
	}()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	done := time.After(time.Duration(timeoutSeconds) * time.Second)
	for {
		select {
		case v, ok := <-sub.Values:
			if !ok {
				return false, nil
			}
			log.Printf("implicit update received: %+v", v)
			return true, nil
		case err, ok := <-sub.Errors:
			if ok {
				log.Printf("implicit warning: %v", err)
			}
		case <-ticker.C:
			log.Printf("waiting for implicit packets...")
		case <-done:
			return false, nil
		}
	}
}

func main() {
	listenAddr := flag.String("listen", "0.0.0.0:2222", "UDP listen address")
	configInstance := flag.Int("instance", 150, "Assembly ConfigInstance")
	outputPoint := flag.Int("out", 1, "Output connection point")
	inputPoint := flag.Int("in", 1, "Input connection point")
	timeout := flag.Int("timeout", 5, "Timeout in seconds")
	mode := flag.String("mode", "tag", "subscribe mode: tag|implicit")
	tagsArg := flag.String("tags", uint32TagName, "comma-separated tags for tag mode (assumed DINT/int32 in this example)")
	batchSize := flag.Int("batch-size", 200, "tag mode batch size")
	batchesPerTick := flag.Int("batches-per-tick", 1, "tag mode batches scanned each poll interval")
	probe := flag.Bool("probe", false, "Probe multiple instance/out/in combinations")
	flag.Parse()

	log.Printf("Testing with ConfigInstance=%d, OutputPoint=%d, InputPoint=%d, Probe=%v, Mode=%s\n", *configInstance, *outputPoint, *inputPoint, *probe, *mode)

	// This is the ROUTING path to the CPU (used by explicit messaging too).
	// It is not the same as the Class1 connection path used by implicit subscribe.
	path, err := gologix.ParsePath("1,2")
	if err != nil {
		log.Fatalf("rockwell cip failed to parse path. error: %v", err)
	}
	controller := gologix.Controller{
		IpAddress: "192.168.50.130",
		Port:      44818,
		Path:      path,
	}
	client := &gologix.Client{
		Controller:         controller,
		VendorId:           vendorIdDefault,
		ConnectionSize:     connSizeLargeDefault,
		AutoConnect:        true,
		KeepAliveAutoStart: false,
		KeepAliveFrequency: time.Second * 30,
		KeepAliveProps:     []gologix.CIPAttribute{1, 2, 3, 4, 10},
		RPI:                rpiDefault,
		SocketTimeout:      socketTimeoutDefault,
		KnownTags:          make(map[string]gologix.KnownTag),
		// ioi_cache:          make(map[string]*tagIOI),
		Logger: slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}
	if err := client.Connect(); err != nil {
		log.Fatalf("connect failed: %v", err)
	}
	defer client.Disconnect()

	// Validate basic reads for the two tags before implicit subscribe setup.
	var uint32Val int32
	if err := client.Read(uint32TagName, &uint32Val); err != nil {
		log.Fatalf("read uint32 tag failed (%s): %v", uint32TagName, err)
	}
	log.Printf("read check ok: %s=%d", uint32TagName, uint32Val)

	if *mode == "tag" {
		tagMap := map[string]any{}
		for _, raw := range strings.Split(*tagsArg, ",") {
			t := strings.TrimSpace(raw)
			if t == "" {
				continue
			}
			// Example assumes listed tags are DINT-compatible and decoded as int32.
			tagMap[t] = int32(0)
		}
		if len(tagMap) == 0 {
			log.Fatalf("no tags provided for tag mode")
		}

		sub, err := gologix.SubscribeTags(client, gologix.TagMultiSubscriptionConfig{
			Tags:           tagMap,
			PollInterval:   200 * time.Millisecond,
			BatchSize:      *batchSize,
			BatchesPerTick: *batchesPerTick,
			EmitInitial:    true,
			ValueBuffer:    256,
			ErrorBuffer:    32,
		})
		if err != nil {
			log.Fatalf("tag subscribe failed: %v", err)
		}
		defer func() {
			if stopErr := sub.Stop(); stopErr != nil {
				log.Printf("tag subscription stop warning: %v", stopErr)
			}
		}()

		done := time.After(time.Duration(*timeout) * time.Second)
		for {
			select {
			case v, ok := <-sub.Values:
				if !ok {
					return
				}
				log.Printf("tag update: %s=%v at %s", v.Tag, v.Value, v.Timestamp.Format(time.RFC3339Nano))
			case subErr, ok := <-sub.Errors:
				if ok {
					log.Printf("tag subscribe warning: %v", subErr)
				}
			case <-done:
				return
			}
		}
	}

	if *mode != "implicit" {
		log.Fatalf("unsupported mode: %s (use tag or implicit)", *mode)
		return
	}

	if *probe {
		cases := []probeCase{
			{instance: 32, out: 1, in: 1},
			{instance: 100, out: 1, in: 1},
			{instance: 150, out: 1, in: 1},
			{instance: 151, out: 1, in: 1},
			{instance: 150, out: 1, in: 2},
			{instance: 150, out: 2, in: 1},
		}

		found := false
		for idx, c := range cases {
			log.Printf("probe %d/%d: instance=%d out=%d in=%d", idx+1, len(cases), c.instance, c.out, c.in)
			received, subErr := runOneSubscription(client, *listenAddr, *timeout, c.instance, c.out, c.in)
			if subErr != nil {
				log.Printf("probe result: FAIL subscribe err=%v", subErr)
				continue
			}
			if received {
				found = true
				log.Printf("probe result: SUCCESS received UDP with instance=%d out=%d in=%d", c.instance, c.out, c.in)
				break
			}
			log.Printf("probe result: NO DATA with instance=%d out=%d in=%d", c.instance, c.out, c.in)
		}

		if !found {
			log.Printf("probe summary: no combination received UDP packets")
		}
		return
	}

	received, err := runOneSubscription(client, *listenAddr, *timeout, *configInstance, *outputPoint, *inputPoint)
	if err != nil {
		log.Fatalf("implicit subscribe failed: %v", err)
	}
	if !received {
		log.Printf("TIMEOUT: No UDP data received after %d seconds", *timeout)
		return
	}
}
