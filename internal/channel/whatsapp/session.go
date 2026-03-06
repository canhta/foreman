package whatsapp

import (
	"context"
	"fmt"
	"os"

	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// LoginWithPairingCode links a WhatsApp account using a pairing code.
// Blocks until the device is linked or context is cancelled.
func LoginWithPairingCode(ctx context.Context, sessionDB, phone string) error {
	container, err := sqlstore.New(ctx, "sqlite3",
		fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)", sessionDB),
		waLog.Noop)
	if err != nil {
		return fmt.Errorf("session db: %w", err)
	}

	deviceStore := container.NewDevice()
	client := whatsmeow.NewClient(deviceStore, waLog.Noop)

	code, err := client.PairPhone(ctx, phone, true, whatsmeow.PairClientChrome, "Chrome (Linux)")
	if err != nil {
		return fmt.Errorf("pair phone: %w", err)
	}

	fmt.Printf("Pairing code: %s\n", code)
	fmt.Println("Open WhatsApp -> Linked Devices -> Link a Device -> Enter code")
	fmt.Println("Waiting for confirmation...")

	if err := client.Connect(); err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	<-ctx.Done()
	client.Disconnect()
	fmt.Println("WhatsApp linked successfully. Session saved.")
	return nil
}

// LoginWithQR links a WhatsApp account using a QR code.
// Blocks until the device is linked or context is cancelled.
func LoginWithQR(ctx context.Context, sessionDB string) error {
	container, err := sqlstore.New(ctx, "sqlite3",
		fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)", sessionDB),
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
