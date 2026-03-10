package whatsapp

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

func logf(format string, args ...any) {
	fmt.Printf("[%s] "+format+"\n", append([]any{time.Now().Format("15:04:05.000")}, args...)...)
}

// LoginWithPairingCode links a WhatsApp account using a pairing code.
// Blocks until the device is linked or context is cancelled.
func LoginWithPairingCode(ctx context.Context, sessionDB, phone string) error {
	logf("Opening session DB: %s", sessionDB)
	container, err := sqlstore.New(ctx, "sqlite3",
		fmt.Sprintf("file:%s?_foreign_keys=on&_busy_timeout=5000", sessionDB),
		waLog.Noop)
	if err != nil {
		return fmt.Errorf("session db: %w", err)
	}

	// Remove stale devices from previous failed/interrupted login attempts.
	existing, err := container.GetAllDevices(ctx)
	if err != nil {
		return fmt.Errorf("get devices: %w", err)
	}
	logf("Found %d existing device(s) in session DB", len(existing))
	for i, d := range existing {
		logf("  Deleting stale device[%d]: JID=%v", i, d.ID)
		if deleteErr := container.DeleteDevice(ctx, d); deleteErr != nil {
			return fmt.Errorf("delete stale device: %w", deleteErr)
		}
	}

	deviceStore := container.NewDevice()
	logf("Created new device store")

	// Use WARN level so internal library noise doesn't bury the pairing code output.
	clientLog := waLog.Stdout("WA", "WARN", true)
	client := whatsmeow.NewClient(deviceStore, clientLog)

	// Register the event handler BEFORE connecting so no events are missed.
	done := make(chan error, 1)
	var (
		mu     sync.Mutex
		pairOK bool
	)
	client.AddEventHandler(func(evt interface{}) {
		logf("Event received: %T", evt)
		switch e := evt.(type) {
		case *events.PairSuccess:
			logf("PairSuccess: ID=%v Platform=%s BusinessName=%q", e.ID, e.Platform, e.BusinessName)
			mu.Lock()
			pairOK = true
			mu.Unlock()
		case *events.PairError:
			logf("PairError: %v", e.Error)
			select {
			case done <- fmt.Errorf("pairing failed: %v", e.Error):
			default:
			}
		case *events.Connected:
			logf("Connected event fired")
			mu.Lock()
			ok := pairOK
			mu.Unlock()
			if ok {
				logf("PairSuccess already seen — sending success")
				select {
				case done <- nil:
				default:
				}
			} else {
				logf("Connected fired but PairSuccess not yet seen — ignoring (existing session reconnect?)")
			}
		case *events.Disconnected:
			logf("Disconnected event fired")
		case *events.LoggedOut:
			logf("LoggedOut event: Reason=%v", e.Reason)
			select {
			case done <- fmt.Errorf("logged out during pairing: %v", e.Reason):
			default:
			}
		default:
			// Log all other events so we can see the full event sequence.
			logf("  (unhandled event type: %T)", evt)
		}
	})

	// Use GetQRChannel to get notified when the WS handshake is complete.
	// PairPhone must only be called after the first QR event arrives.
	qrChan, err := client.GetQRChannel(ctx)
	if err != nil {
		return fmt.Errorf("get QR channel: %w", err)
	}
	logf("QR channel obtained, connecting...")

	if connectErr := client.Connect(); connectErr != nil {
		return fmt.Errorf("connect: %w", connectErr)
	}
	logf("Connect() returned (WebSocket dialing in background)")

	// Wait for the first QR event — this signals the WS handshake is complete
	// and the server is ready to accept PairPhone.
	logf("Waiting for first QR channel event...")
	select {
	case qrEvt := <-qrChan:
		logf("QR channel event received: Event=%q Code(len)=%d", qrEvt.Event, len(qrEvt.Code))
		if qrEvt.Event == "timeout" {
			client.Disconnect()
			return fmt.Errorf("connection timed out before QR was ready")
		}
	case <-ctx.Done():
		client.Disconnect()
		return ctx.Err()
	}

	logf("Requesting pairing code for phone=%s", phone)
	code, err := client.PairPhone(ctx, phone, true, whatsmeow.PairClientChrome, "Chrome (macOS)")
	if err != nil {
		client.Disconnect()
		return fmt.Errorf("pair phone: %w", err)
	}
	logf("PairPhone returned code successfully")

	fmt.Println()
	fmt.Println("╔══════════════════════════════════════╗")
	fmt.Printf( "║  PAIRING CODE:  %-20s  ║\n", code)
	fmt.Println("╚══════════════════════════════════════╝")
	fmt.Println("Open WhatsApp → Linked Devices → Link a Device → Enter code above")
	fmt.Println("Waiting for confirmation (up to 5 minutes)...")
	fmt.Println()

	// Two-stage completion: PairSuccess means the code was accepted by the server;
	// Connected means the full post-pairing handshake finished.
	select {
	case err := <-done:
		client.Disconnect()
		if err != nil {
			logf("Pairing failed: %v", err)
			return err
		}
		logf("Pairing completed successfully")
		fmt.Println("WhatsApp linked successfully. Session saved.")
		return nil
	case <-ctx.Done():
		logf("Context cancelled while waiting for pairing confirmation")
		client.Disconnect()
		return ctx.Err()
	}
}

// LoginWithQR links a WhatsApp account using a QR code.
// Blocks until the device is linked or context is cancelled.
func LoginWithQR(ctx context.Context, sessionDB string) error {
	logf("Opening session DB: %s", sessionDB)
	container, err := sqlstore.New(ctx, "sqlite3",
		fmt.Sprintf("file:%s?_foreign_keys=on&_busy_timeout=5000", sessionDB),
		waLog.Noop)
	if err != nil {
		return fmt.Errorf("session db: %w", err)
	}

	deviceStore := container.NewDevice()
	clientLog := waLog.Stdout("WA", "WARN", true)
	client := whatsmeow.NewClient(deviceStore, clientLog)

	client.AddEventHandler(func(evt interface{}) {
		logf("QR-flow event: %T", evt)
	})

	qrChan, err := client.GetQRChannel(ctx)
	if err != nil {
		return fmt.Errorf("get QR channel: %w", err)
	}
	if err := client.Connect(); err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	for evt := range qrChan {
		logf("QR channel event: %q", evt.Event)
		switch evt.Event {
		case "code":
			fmt.Println("Scan the QR code below with WhatsApp -> Linked Devices -> Link a Device")
			qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
		case "success":
			logf("QR scan succeeded")
			fmt.Println("WhatsApp linked successfully. Session saved.")
			client.Disconnect()
			return nil
		case "timeout":
			client.Disconnect()
			return fmt.Errorf("QR code expired — please try again")
		default:
			logf("Unknown QR channel event: %q", evt.Event)
		}
	}

	client.Disconnect()
	return nil
}
