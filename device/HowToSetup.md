# Raspberry Pi Zero 2 W: USB Ethernet, I2C, and ATECC608A Setup

This guide provides minimal instructions for programmers to enable USB Ethernet gadget mode and I2C on a Raspberry Pi Zero 2 W, along with specific soldering instructions for connecting an ATECC608A secure element for true random number generation and hashing.

<!-- TOC -->
* [Raspberry Pi Zero 2 W: USB Ethernet, I2C, and ATECC608A Setup](#raspberry-pi-zero-2-w-usb-ethernet-i2c-and-atecc608a-setup)
  * [1. Hardware Connection (Soldering ATECC608A)](#1-hardware-connection-soldering-atecc608a)
  * [2. Prepare the SD Card `boot` Partition](#2-prepare-the-sd-card-boot-partition)
    * [Edit `config.txt`](#edit-configtxt)
    * [Edit `cmdline.txt`](#edit-cmdlinetxt)
    * [Create `ssh` File](#create-ssh-file)
  * [3. Boot the Raspberry Pi](#3-boot-the-raspberry-pi)
  * [4. Initial Access via USB Ethernet](#4-initial-access-via-usb-ethernet)
    * [Find the Pi's IP Address](#find-the-pis-ip-address)
    * [SSH to the Pi](#ssh-to-the-pi)
  * [5. Configure I2C and ATECC608A on the Pi](#5-configure-i2c-and-atecc608a-on-the-pi)
    * [Install I2C Tools](#install-i2c-tools)
    * [Verify I2C and ATECC608A Detection](#verify-i2c-and-atecc608a-detection)
<!-- TOC -->

## 1. Hardware Connection (Soldering ATECC608A)

**Safety First:** Ensure your Raspberry Pi Zero 2 W is **powered off and disconnected from all power sources** before performing any soldering.

You will be connecting the ATECC608A to the Raspberry Pi's GPIO header. Refer to a Raspberry Pi GPIO pinout diagram if you're unsure of pin locations.

| ATECC608A Pin | Raspberry Pi GPIO Pin | Pi Pin Number | Function    |
| :------------ | :-------------------- | :------------ | :---------- |
| VCC           | 3V3                   | Pin 1         | Power       |
| SDA           | GPIO 2 (SDA)          | Pin 3         | I2C Data    |
| SCL           | GPIO 3 (SCL)          | Pin 5         | I2C Clock   |
| GND           | Ground                | Pin 6         | Ground      |

<img src="docs/wiring.jpeg" alt="Endresult with a little heatsink"/>


**Soldering Steps:**

1.  **Prepare Wires:** Cut four small lengths of wire. Strip a small amount of insulation (a few millimeters) from both ends of each wire.
2.  **Tin Wires:** Lightly tin the stripped ends of the wires with solder. This makes soldering easier and creates more reliable connections.
3.  **Solder to ATECC608A:** Carefully solder one end of each wire to the corresponding VCC, SDA, SCL, and GND pads on your ATECC608A module or chip breakout board.
4.  **Solder to Raspberry Pi:** Solder the other end of each wire to the designated pins on the Raspberry Pi Zero 2 W's GPIO header as per the table above.
    * **Pin 1:** 3V3 (closest to the corner with the square pad)
    * **Pin 3:** GPIO 2 (I2C SDA)
    * **Pin 5:** GPIO 3 (I2C SCL)
    * **Pin 6:** Ground
5.  **Inspect:** Thoroughly double-check all your connections. Ensure solder joints are shiny and conical, and critically, that there are no solder bridges (shorts) between adjacent pins.

## 2. Prepare the SD Card `boot` Partition

Access the `boot` partition of your flashed SD card on your computer.

### Edit `config.txt`

Add the following lines to `boot/config.txt`:

```
dtoverlay=dwc2
dtparam=i2c_arm=on,i2c_vc=on
```

### Edit `cmdline.txt`

**Append** `modules-load=dwc2,g_ether` to the **existing single line** in `boot/cmdline.txt`. Ensure there are no line breaks and that it is space-separated from other arguments.

For example, your `cmdline.txt` might look something like this (your `PARTUUID` will differ):

```
console=serial0,115200 console=tty1 root=PARTUUID=xxxxxxxx-xx rootfstype=ext4 fsck.repair=yes rootwait modules-load=dwc2,g_ether quiet init=/usr/lib/raspi-config/init_resize.sh
```

### Create `ssh` File

Create an empty file named `ssh` (with no file extension) in the `boot` directory. This enables SSH access on first boot.

## 3. Boot the Raspberry Pi

1.  Safely unmount the SD card from your computer.
2.  Insert the SD card into your Raspberry Pi Zero 2 W.
3.  Connect the **data/power micro-USB port** (the one closer to the edge, *not* the dedicated power port if present) of the Pi to your computer using a reliable USB **data** cable.

## 4. Initial Access via USB Ethernet

Your computer should now recognize the Raspberry Pi as a new USB Ethernet device. This may take a few moments for the drivers to load and an IP address to be assigned.

### Find the Pi's IP Address

* **Linux/macOS:** Open a terminal and use `ifconfig` or `ip addr`. Look for a new interface (e.g., `usb0`). Alternatively, if your host machine supports mDNS/Bonjour, try `ping raspberrypi.local`.
* **Windows:** Open "Network Connections" (Control Panel -> Network and Sharing Center -> Change adapter settings) and look for a new adapter.

Common default IP ranges for the Pi in gadget mode include `192.168.137.x`, `192.168.7.x`, or `169.254.x.x` (APIPA).

### SSH to the Pi

Once you've identified the Pi's IP address, you can establish an SSH connection:

```bash
ssh pi@<Pi_IP_Address>
```

The default password for the `pi` user is `raspberry`.

## 5. Configure I2C and ATECC608A on the Pi

After successfully SSHing into your Raspberry Pi, run the following commands:

### Install I2C Tools

```bash
sudo apt update
sudo apt install -y i2c-tools
sudo adduser pi i2c
```

**Note:** Replace `pi` with your actual username if you're not using the default `pi` user. You may need to log out and back in (or reboot) for the new group membership to take effect.

### Verify I2C and ATECC608A Detection

* **I2C Kernel Modules:**
    ```bash
    lsmod | grep i2c
    ```
  You should see `i2c_bcm2835` and `i2c_dev` listed. This confirms the I2C kernel modules are loaded.

* **I2C Bus Scan (Detect ATECC608A):**
    ```bash
    i2cdetect -y 1
    ```
  This command scans the main I2C bus (bus 1). You should see `60` (the default I2C address for the ATECC608A) displayed in the grid, confirming the device is detected and communicating over I2C. r cryptographic applications!