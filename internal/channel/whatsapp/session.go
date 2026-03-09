package whatsapp

import (
	"context"
	"fmt"
	"os"

	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// LoginWithPairingCode links a WhatsApp account using a pairing code.
// Blocks until the device is linked or context is cancelled.
func LoginWithPairingCode(ctx context.Context, sessionDB, phone string) error {
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
	for _, d := range existing {
		if deleteErr := container.DeleteDevice(ctx, d); deleteErr != nil {
			return fmt.Errorf("delete stale device: %w", deleteErr)
		}
	}

	deviceStore := container.NewDevice()
	client := whatsmeow.NewClient(deviceStore, waLog.Noop)

	// Use GetQRChannel to get notified when the connection is fully established.
	// PairPhone must only be called after the first QR event arrives.
	qrChan, err := client.GetQRChannel(ctx)
	if err != nil {
		return fmt.Errorf("get QR channel: %w", err)
	}

	if connectErr := client.Connect(); connectErr != nil {
		return fmt.Errorf("connect: %w", connectErr)
	}

	// Wait for the first QR event — this signals the handshake is complete.
	select {
	case <-qrChan:
	case <-ctx.Done():
		client.Disconnect()
		return ctx.Err()
	}

	code, err := client.PairPhone(ctx, phone, true, whatsmeow.PairClientChrome, "Chrome (macOS)")
	if err != nil {
		client.Disconnect()
		return fmt.Errorf("pair phone: %w", err)
	}

	fmt.Printf("Pairing code: %s\n", code)
	fmt.Println("Open WhatsApp -> Linked Devices -> Link a Device -> Enter code")
	fmt.Println("Waiting for confirmation...")

	// Two-stage completion: PairSuccess means the code was accepted; Connected means
	// the full post-pairing handshake finished (phone shows "Logged in" at that point).
	done := make(chan error, 1)
	var pairOK bool
	client.AddEventHandler(func(evt interface{}) {
		switch e := evt.(type) {
		case *events.PairSuccess:
			_ = e
			pairOK = true
		case *events.PairError:
			done <- fmt.Errorf("pairing failed: %v", e.Error)
		case *events.Connected:
			if pairOK {
				done <- nil
			}
		}
	})

	select {
	case err := <-done:
		client.Disconnect()
		if err != nil {
			return err
		}
		fmt.Println("WhatsApp linked successfully. Session saved.")
		return nil
	case <-ctx.Done():
		client.Disconnect()
		return ctx.Err()
	}
}

// LoginWithQR links a WhatsApp account using a QR code.
// Blocks until the device is linked or context is cancelled.
func LoginWithQR(ctx context.Context, sessionDB string) error {
	container, err := sqlstore.New(ctx, "sqlite3",
		fmt.Sprintf("file:%s?_foreign_keys=on&_busy_timeout=5000", sessionDB),
		waLog.Noop)
	if err != nil {
		return fmt.Errorf("session db: %w", err)
	}

	deviceStore := container.NewDevice()
	client := whatsmeow.NewClient(deviceStore, waLog.Noop)

	qrChan, err := client.GetQRChannel(ctx)
	if err != nil {
		return fmt.Errorf("get QR channel: %w", err)
	}
	if err := client.Connect(); err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	for evt := range qrChan {
		switch evt.Event {
		case "code":
			fmt.Println("Scan the QR code below with WhatsApp -> Linked Devices -> Link a Device")
			qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
		case "success":
			fmt.Println("WhatsApp linked successfully. Session saved.")
			client.Disconnect()
			return nil
		case "timeout":
			client.Disconnect()
			return fmt.Errorf("QR code expired — please try again")
		}
	}

	client.Disconnect()
	return nil
}
