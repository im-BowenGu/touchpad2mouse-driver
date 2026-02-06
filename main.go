package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"strings"
	"syscall"
	"time"
	"unsafe"

	evdev "github.com/gvalkov/golang-evdev"
)

const (
	DeviceNameKeyword     = "GXTP"
	DeviceNameMustContain = "Touchpad"

	MoveSensitivity  = 0.6
	AccelFactor      = 1.5
	ScrollDivider    = 40.0
	NaturalScrolling = true

	PalmZoneTopY          = 500
	PalmPressureThreshold = 45

	MinMovePressure      = 2
	LowPressureThreshold = 15
	SmallMoveCutoff      = 2.0

	TapTimeout          = 200 * time.Millisecond
	TapMovementLimit    = 40.0
	PressThreshold      = 140
	ReleaseThreshold    = 80
	CooldownAfterScroll = 250 * time.Millisecond

	GestureDistThreshold = 100.0

	RightClickZoneX = 3000
	BottomZoneY     = 1800
)

const (
	EV_SYN = 0x00
	EV_KEY = 0x01
	EV_REL = 0x02

	SYN_REPORT = 0x00

	REL_X      = 0x00
	REL_Y      = 0x01
	REL_HWHEEL = 0x06
	REL_WHEEL  = 0x08

	BTN_LEFT   = 0x110
	BTN_RIGHT  = 0x111
	BTN_MIDDLE = 0x112

	KEY_LEFTMETA  = 125
	KEY_LEFTALT   = 56
	KEY_LEFTSHIFT = 42
	KEY_TAB       = 15
	KEY_D         = 32

	UINPUT_MAX_NAME_SIZE = 80

	UI_SET_EVBIT  = 0x40045564
	UI_SET_KEYBIT = 0x40045565
	UI_SET_RELBIT = 0x40045566
	UI_DEV_CREATE = 0x5501
)

type inputEvent struct {
	Time  syscall.Timeval
	Type  uint16
	Code  uint16
	Value int32
}

type uinputUserDev struct {
	Name       [UINPUT_MAX_NAME_SIZE]byte
	ID         inputID
	EffectsMax uint32
	Absmax     [64]int32
	Absmin     [64]int32
	Absfuzz    [64]int32
	Absflat    [64]int32
}

type inputID struct {
	Bustype uint16
	Vendor  uint16
	Product uint16
	Version uint16
}

type Slot struct {
	X, Y, P int32
}

type VirtualDevice struct {
	fd *os.File
}

func ioctl(fd uintptr, request uintptr, val uintptr) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, request, val)
	if errno != 0 {
		return errno
	}
	return nil
}

func ioctlInt(fd uintptr, request uintptr, val int) error {
	return ioctl(fd, request, uintptr(val))
}

func createVirtualDevice(name string) (*VirtualDevice, error) {
	f, err := os.OpenFile("/dev/uinput", os.O_WRONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("open /dev/uinput: %w", err)
	}

	fd := f.Fd()

	for _, ev := range []int{EV_KEY, EV_REL, EV_SYN} {
		if err := ioctlInt(fd, UI_SET_EVBIT, ev); err != nil {
			f.Close()
			return nil, fmt.Errorf("set evbit %d: %w", ev, err)
		}
	}

	for _, rel := range []int{REL_X, REL_Y, REL_WHEEL, REL_HWHEEL} {
		if err := ioctlInt(fd, UI_SET_RELBIT, rel); err != nil {
			f.Close()
			return nil, fmt.Errorf("set relbit %d: %w", rel, err)
		}
	}

	for _, key := range []int{BTN_LEFT, BTN_RIGHT, BTN_MIDDLE, KEY_LEFTMETA, KEY_TAB, KEY_LEFTALT, KEY_LEFTSHIFT, KEY_D} {
		if err := ioctlInt(fd, UI_SET_KEYBIT, key); err != nil {
			f.Close()
			return nil, fmt.Errorf("set keybit %d: %w", key, err)
		}
	}

	var dev uinputUserDev
	copy(dev.Name[:], name)
	dev.ID.Bustype = 0x03
	dev.ID.Vendor = 0x1234
	dev.ID.Product = 0x5678
	dev.ID.Version = 1

	buf := (*[4096]byte)(unsafe.Pointer(&dev))[:unsafe.Sizeof(dev)]
	if _, err := f.Write(buf); err != nil {
		f.Close()
		return nil, fmt.Errorf("write dev info: %w", err)
	}

	if err := ioctl(fd, UI_DEV_CREATE, 0); err != nil {
		f.Close()
		return nil, fmt.Errorf("dev create: %w", err)
	}

	time.Sleep(200 * time.Millisecond)
	return &VirtualDevice{fd: f}, nil
}

func (v *VirtualDevice) writeEvent(typ uint16, code uint16, value int32) {
	var tv syscall.Timeval
	syscall.Gettimeofday(&tv)
	binary.Write(v.fd, binary.LittleEndian, inputEvent{Time: tv, Type: typ, Code: code, Value: value})
}

func (v *VirtualDevice) syn() {
	v.writeEvent(EV_SYN, SYN_REPORT, 0)
}

func (v *VirtualDevice) Close() {
	v.fd.Close()
}

func findDevice(keyword, mustContain string) (string, error) {
	devices, _ := evdev.ListInputDevices()
	var fallback string
	for _, dev := range devices {
		nameLower := strings.ToLower(dev.Name)
		if strings.Contains(nameLower, strings.ToLower(keyword)) {
			if strings.Contains(nameLower, strings.ToLower(mustContain)) {
				return dev.Fn, nil
			}
			if fallback == "" {
				fallback = dev.Fn
			}
		}
	}
	if fallback != "" {
		return fallback, nil
	}
	return "", fmt.Errorf("device with keyword '%s' not found", keyword)
}

func main() {
	devicePath, err := findDevice(DeviceNameKeyword, DeviceNameMustContain)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Found touchpad at %s\n", devicePath)

	dev, err := evdev.Open(devicePath)
	if err != nil {
		fmt.Printf("Error opening device: %v\n", err)
		os.Exit(1)
	}
	dev.Grab()
	defer dev.Release()

	vmouse, err := createVirtualDevice("Goodix-Driver")
	if err != nil {
		fmt.Printf("Error creating virtual device: %v\n", err)
		os.Exit(1)
	}
	defer vmouse.Close()

	slots := make(map[int]*Slot)
	prevSlots := make(map[int]*Slot)
	activeSlot := 0

	var (
		currentFingerCount     int
		maxFingersDuringTouch  int
		maxPressureDuringTouch int32
		touchStartTime         time.Time
		touchStartX, touchStartY int32
		isPhysicallyClicked    bool
		activePhysicalButton   uint16
		lastScrollTime         time.Time
		scrollAccX, scrollAccY float64
		isScrolling            bool
		isPalmRejected         bool
		gestureAccX, gestureAccY float64
		gestureTriggered       bool
	)

	fmt.Println("Driver started.")

	for {
		events, err := dev.Read()
		if err != nil {
			break
		}

		for _, event := range events {
			switch event.Type {
			case evdev.EV_ABS:
				if event.Code == evdev.ABS_MT_SLOT {
					activeSlot = int(event.Value)
				}
				if _, ok := slots[activeSlot]; !ok {
					slots[activeSlot] = &Slot{}
				}
				switch event.Code {
				case evdev.ABS_MT_POSITION_X:
					slots[activeSlot].X = event.Value
				case evdev.ABS_MT_POSITION_Y:
					slots[activeSlot].Y = event.Value
				case evdev.ABS_MT_PRESSURE:
					slots[activeSlot].P = event.Value
					if event.Value > maxPressureDuringTouch {
						maxPressureDuringTouch = event.Value
					}
				case evdev.ABS_MT_TRACKING_ID:
					if event.Value == -1 {
						delete(slots, activeSlot)
					}
				}

			case evdev.EV_KEY:
				switch event.Code {
				case evdev.BTN_TOOL_FINGER:
					if event.Value == 1 { currentFingerCount = 1 } else { currentFingerCount = 0 }
				case evdev.BTN_TOOL_DOUBLETAP:
					if event.Value == 1 { currentFingerCount = 2 } else { currentFingerCount = 0 }
				case evdev.BTN_TOOL_TRIPLETAP:
					if event.Value == 1 { currentFingerCount = 3 } else { currentFingerCount = 0 }
				}
				if currentFingerCount > maxFingersDuringTouch {
					maxFingersDuringTouch = currentFingerCount
				}

				if event.Code == evdev.BTN_TOUCH {
					now := time.Now()
					if event.Value == 1 {
						touchStartTime = now
						maxFingersDuringTouch = currentFingerCount
						maxPressureDuringTouch = 0
						isScrolling = false
						gestureTriggered = false
						gestureAccX, gestureAccY = 0, 0
						if s, ok := slots[0]; ok {
							touchStartX, touchStartY = s.X, s.Y
							isPalmRejected = s.Y < PalmZoneTopY && s.P > PalmPressureThreshold
						}
						prevSlots = make(map[int]*Slot)
					} else {
						duration := now.Sub(touchStartTime)
						timeSinceScroll := now.Sub(lastScrollTime)
						wasPhysicalClick := maxPressureDuringTouch > PressThreshold

						if !isPalmRejected && duration < TapTimeout && !wasPhysicalClick &&
							timeSinceScroll > CooldownAfterScroll && !gestureTriggered {

							lastX, lastY := touchStartX, touchStartY
							if ps, ok := prevSlots[0]; ok {
								lastX, lastY = ps.X, ps.Y
							}
							dist := math.Sqrt(math.Pow(float64(lastX-touchStartX), 2) + math.Pow(float64(lastY-touchStartY), 2))

							if dist < TapMovementLimit {
								clickBtn := uint16(BTN_LEFT)
								if maxFingersDuringTouch == 2 {
									clickBtn = BTN_RIGHT
								} else if maxFingersDuringTouch == 3 {
									clickBtn = BTN_MIDDLE
								} else if lastX > RightClickZoneX && lastY > BottomZoneY {
									clickBtn = BTN_RIGHT
								}
								vmouse.writeEvent(EV_KEY, clickBtn, 1)
								vmouse.syn()
								time.Sleep(15 * time.Millisecond)
								vmouse.writeEvent(EV_KEY, clickBtn, 0)
								vmouse.syn()
							}
						}
					}
				}

			case evdev.EV_SYN:
				if event.Code == evdev.SYN_REPORT {
					if isPalmRejected {
						for k, v := range slots {
							prevSlots[k] = &Slot{X: v.X, Y: v.Y, P: v.P}
						}
						continue
					}

					pressure := int32(0)
					if s, ok := slots[0]; ok {
						pressure = s.P
					}

					if !isPhysicallyClicked && pressure > PressThreshold {
						isPhysicallyClicked = true
						activePhysicalButton = BTN_LEFT
						if s, ok := slots[0]; ok && s.X > RightClickZoneX && s.Y > BottomZoneY {
							activePhysicalButton = BTN_RIGHT
						}
						vmouse.writeEvent(EV_KEY, activePhysicalButton, 1)
						vmouse.syn()
					} else if isPhysicallyClicked && pressure < ReleaseThreshold {
						isPhysicallyClicked = false
						vmouse.writeEvent(EV_KEY, activePhysicalButton, 0)
						vmouse.syn()
						activePhysicalButton = 0
					}

					s0, hasS0 := slots[0]
					p0, hasP0 := prevSlots[0]

					if hasS0 && hasP0 {
						dx := float64(s0.X - p0.X)
						dy := float64(s0.Y - p0.Y)

						if currentFingerCount == 3 && !gestureTriggered {
							gestureAccX += dx
							gestureAccY += dy

							if gestureAccX > GestureDistThreshold {
								vmouse.writeEvent(EV_KEY, KEY_LEFTALT, 1)
								vmouse.writeEvent(EV_KEY, KEY_LEFTSHIFT, 1)
								vmouse.writeEvent(EV_KEY, KEY_TAB, 1)
								vmouse.syn()
								time.Sleep(50 * time.Millisecond)
								vmouse.writeEvent(EV_KEY, KEY_TAB, 0)
								vmouse.writeEvent(EV_KEY, KEY_LEFTSHIFT, 0)
								vmouse.writeEvent(EV_KEY, KEY_LEFTALT, 0)
								vmouse.syn()
								gestureTriggered = true
							} else if gestureAccX < -GestureDistThreshold {
								vmouse.writeEvent(EV_KEY, KEY_LEFTALT, 1)
								vmouse.writeEvent(EV_KEY, KEY_TAB, 1)
								vmouse.syn()
								time.Sleep(50 * time.Millisecond)
								vmouse.writeEvent(EV_KEY, KEY_TAB, 0)
								vmouse.writeEvent(EV_KEY, KEY_LEFTALT, 0)
								vmouse.syn()
								gestureTriggered = true
							} else if gestureAccY < -GestureDistThreshold {
								vmouse.writeEvent(EV_KEY, KEY_LEFTMETA, 1)
								vmouse.syn()
								time.Sleep(50 * time.Millisecond)
								vmouse.writeEvent(EV_KEY, KEY_LEFTMETA, 0)
								vmouse.syn()
								gestureTriggered = true
							} else if gestureAccY > GestureDistThreshold {
								vmouse.writeEvent(EV_KEY, KEY_LEFTMETA, 1)
								vmouse.writeEvent(EV_KEY, KEY_D, 1)
								vmouse.syn()
								time.Sleep(50 * time.Millisecond)
								vmouse.writeEvent(EV_KEY, KEY_D, 0)
								vmouse.writeEvent(EV_KEY, KEY_LEFTMETA, 0)
								vmouse.syn()
								gestureTriggered = true
							}

						} else if currentFingerCount == 2 {
							isScrolling = true
							scrollAccY += dy
							scrollAccX += dx
							direction := 1
							if !NaturalScrolling {
								direction = -1
							}

							if math.Abs(scrollAccY) > ScrollDivider {
								ticks := int(scrollAccY / ScrollDivider)
								vmouse.writeEvent(EV_REL, REL_WHEEL, int32(ticks*direction))
								scrollAccY -= float64(ticks) * ScrollDivider
								lastScrollTime = time.Now()
							}
							if math.Abs(scrollAccX) > ScrollDivider {
								ticks := int(scrollAccX / ScrollDivider)
								vmouse.writeEvent(EV_REL, REL_HWHEEL, int32(ticks*-direction))
								scrollAccX -= float64(ticks) * ScrollDivider
								lastScrollTime = time.Now()
							}

						} else if currentFingerCount == 1 && !isScrolling && !gestureTriggered {
							currP := s0.P
							moveDist := math.Abs(dx) + math.Abs(dy)

							if currP >= MinMovePressure &&
								!(currP < LowPressureThreshold && moveDist < SmallMoveCutoff) &&
								math.Abs(dx) < 400 && math.Abs(dy) < 400 {
								accel := 1.0
								if moveDist > 15 {
									accel = AccelFactor
								}
								mx := int32(dx * MoveSensitivity * accel)
								my := int32(dy * MoveSensitivity * accel)
								if mx != 0 || my != 0 {
									vmouse.writeEvent(EV_REL, REL_X, mx)
									vmouse.writeEvent(EV_REL, REL_Y, my)
								}
							}
						}
					}

					vmouse.syn()

					prevSlots = make(map[int]*Slot)
					for k, v := range slots {
						prevSlots[k] = &Slot{X: v.X, Y: v.Y, P: v.P}
					}
				}
			}
		}
	}
}