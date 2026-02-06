import evdev
from evdev import uinput, ecodes as e
import sys
import time
import math
import os

# --- CONFIGURATION ---
DEVICE_NAME_KEYWORD = "GXTP"

# Movement & Physics
MOVE_SENSITIVITY = 0.6
ACCEL_FACTOR = 1.5
SCROLL_DIVIDER = 40
NATURAL_SCROLLING = True   # Applies to both Vertical and Horizontal

# Palm Rejection (NEW)
PALM_ZONE_TOP_Y = 500      # Top area of touchpad (0-500) to monitor for palms
PALM_PRESSURE_THRESHOLD = 45 # Pressure required to trigger palm rejection
PALM_QUICK_TAP_TIME = 0.3  # Max time to consider a "quick" palm brush

# Noise / Accidental Swipe Filtering
MIN_MOVE_PRESSURE = 2      
LOW_PRESSURE_THRESHOLD = 15  
SMALL_MOVE_CUTOFF = 2        

# Tapping & Clicking
TAP_TIMEOUT = 0.20 
TAP_MOVEMENT_LIMIT = 40
PRESS_THRESHOLD = 140
RELEASE_THRESHOLD = 80
COOLDOWN_AFTER_SCROLL = 0.25

# 3-Finger Gestures (NEW)
GESTURE_DISTANCE_THRESHOLD = 100 # Minimum swipe distance to trigger gesture

# Zones
RIGHT_CLICK_ZONE_X = 3000
BOTTOM_ZONE_Y = 1800
# ---------------------

def get_device():
    devices = [evdev.InputDevice(path) for path in evdev.list_devices()]
    for dev in devices:
        if DEVICE_NAME_KEYWORD.lower() in dev.name.lower():
            print(f"Found {dev.name} at {dev.path}")
            dev.grab()
            return dev
    print(f"Error: No device with keyword '{DEVICE_NAME_KEYWORD}' found.")
    sys.exit(1)

# Added keyboard keys for gestures
cap = {
    e.EV_REL: (e.REL_X, e.REL_Y, e.REL_WHEEL, e.REL_HWHEEL),
    e.EV_KEY: (e.BTN_LEFT, e.BTN_RIGHT, 
               e.KEY_LEFTMETA, e.KEY_TAB, e.KEY_LEFTALT, e.KEY_LEFTSHIFT, e.KEY_D)
}

def main():
    if not os.path.exists('/dev/uinput'):
        print("Error: /dev/uinput not found. Run 'sudo modprobe uinput'")
        sys.exit(1)

    touchpad = get_device()
    
    with uinput.UInput(cap, name='Goodix-Gemini-Driver') as vmouse:
        slots = {}
        prev_slots = {}
        active_slot = 0
        
        current_finger_count = 0
        max_fingers_during_touch = 0
        max_pressure_during_touch = 0  
        
        touch_start_time = 0
        touch_start_coords = (0, 0)
        
        is_physically_clicked = False
        active_physical_button = None 
        
        last_scroll_time = 0
        scroll_acc_y = 0
        scroll_acc_x = 0
        is_scrolling = False
        
        # New State Variables
        is_palm_rejected = False  # Flag to suppress entire touch event
        gesture_acc_x = 0         # Accumulator for 3-finger X movement
        gesture_acc_y = 0         # Accumulator for 3-finger Y movement
        gesture_triggered = False # Prevent multiple triggers per swipe

        for event in touchpad.read_loop():
            if event.type == e.EV_ABS:
                if event.code == e.ABS_MT_SLOT:
                    active_slot = event.value
                
                if active_slot not in slots:
                    slots[active_slot] = {'x': 0, 'y': 0, 'p': 0}

                if event.code == e.ABS_MT_POSITION_X:
                    slots[active_slot]['x'] = event.value
                elif event.code == e.ABS_MT_POSITION_Y:
                    slots[active_slot]['y'] = event.value
                elif event.code == e.ABS_MT_PRESSURE:
                    slots[active_slot]['p'] = event.value
                    if event.value > max_pressure_during_touch:
                        max_pressure_during_touch = event.value
                elif event.code == e.ABS_MT_TRACKING_ID:
                    if event.value == -1:
                        if active_slot in slots: del slots[active_slot]

            elif event.type == e.EV_KEY:
                if event.code == e.BTN_TOOL_FINGER: current_finger_count = 1 if event.value else 0
                elif event.code == e.BTN_TOOL_DOUBLETAP: current_finger_count = 2 if event.value else 0
                elif event.code == e.BTN_TOOL_TRIPLETAP: current_finger_count = 3 if event.value else 0
                elif event.code == e.BTN_TOOL_QUADTAP: current_finger_count = 4 if event.value else 0
                
                if current_finger_count > max_fingers_during_touch:
                    max_fingers_during_touch = current_finger_count

                if event.code == e.BTN_TOUCH:
                    now = time.time()
                    if event.value == 1: # Touch Start
                        touch_start_time = now
                        max_fingers_during_touch = current_finger_count
                        max_pressure_during_touch = 0
                        is_scrolling = False
                        gesture_triggered = False
                        gesture_acc_x = 0
                        gesture_acc_y = 0
                        
                        if 0 in slots:
                            touch_start_coords = (slots[0]['x'], slots[0]['y'])
                            
                            # --- PALM REJECTION LOGIC ---
                            # If touch starts in top zone AND has high pressure (palm), ignore it
                            if (slots[0]['y'] < PALM_ZONE_TOP_Y and 
                                slots[0]['p'] > PALM_PRESSURE_THRESHOLD):
                                is_palm_rejected = True
                            else:
                                is_palm_rejected = False
                        
                        prev_slots.clear()
                    
                    else: # Touch End (Release)
                        duration = now - touch_start_time
                        time_since_scroll = now - last_scroll_time
                        was_physical_click = (max_pressure_during_touch > PRESS_THRESHOLD)
                        
                        # --- CLICK LOGIC (Only if NOT a palm) ---
                        if not is_palm_rejected:
                            if (duration < TAP_TIMEOUT and not was_physical_click and 
                                time_since_scroll > COOLDOWN_AFTER_SCROLL and 
                                not gesture_triggered):
                                
                                last_x, last_y = touch_start_coords
                                if 0 in prev_slots:
                                    last_x, last_y = prev_slots[0]['x'], prev_slots[0]['y']
                                    
                                dist = math.sqrt((last_x - touch_start_coords[0])**2 + (last_y - touch_start_coords[1])**2)
                                
                                if dist < TAP_MOVEMENT_LIMIT:
                                    click_btn = e.BTN_LEFT
                                    if max_fingers_during_touch == 2:
                                        click_btn = e.BTN_RIGHT
                                    elif max_fingers_during_touch == 3:
                                        click_btn = e.BTN_MIDDLE # Optional: 3 finger tap
                                    elif last_x > RIGHT_CLICK_ZONE_X and last_y > BOTTOM_ZONE_Y:
                                        click_btn = e.BTN_RIGHT
                                    
                                    # Perform Click
                                    vmouse.write(e.EV_KEY, click_btn, 1)
                                    vmouse.syn()
                                    time.sleep(0.015)
                                    vmouse.write(e.EV_KEY, click_btn, 0)
                                    vmouse.syn()

            elif event.type == e.EV_SYN and event.code == e.SYN_REPORT:
                now = time.time()
                pressure = slots[0]['p'] if 0 in slots else 0
                
                # If identified as a palm, do absolutely nothing (no moves, no clicks)
                if is_palm_rejected:
                    continue

                # Physical Click Logic
                if not is_physically_clicked and pressure > PRESS_THRESHOLD:
                    is_physically_clicked = True
                    is_right = (slots[0]['x'] > RIGHT_CLICK_ZONE_X and slots[0]['y'] > BOTTOM_ZONE_Y) if 0 in slots else False
                    active_physical_button = e.BTN_RIGHT if is_right else e.BTN_LEFT
                    vmouse.write(e.EV_KEY, active_physical_button, 1)
                
                elif is_physically_clicked and pressure < RELEASE_THRESHOLD:
                    is_physically_clicked = False
                    if active_physical_button:
                        vmouse.write(e.EV_KEY, active_physical_button, 0)
                        active_physical_button = None

                # Movement, Scrolling & Gestures
                if 0 in slots and 0 in prev_slots:
                    dx = slots[0]['x'] - prev_slots[0]['x']
                    dy = slots[0]['y'] - prev_slots[0]['y']
                    
                    # --- 3 FINGER GESTURES ---
                    if current_finger_count == 3 and not gesture_triggered:
                        gesture_acc_x += dx
                        gesture_acc_y += dy
                        
                        # Swipe Right (Alt+Tab)
                        if gesture_acc_x > GESTURE_DISTANCE_THRESHOLD: 
                            vmouse.write(e.EV_KEY, e.KEY_LEFTALT, 1)
                            vmouse.write(e.EV_KEY, e.KEY_LEFTSHIFT, 1)
                            vmouse.write(e.EV_KEY, e.KEY_TAB, 1)
                            vmouse.syn()
                            time.sleep(0.05)
                            vmouse.write(e.EV_KEY, e.KEY_TAB, 0)
                            vmouse.write(e.EV_KEY, e.KEY_LEFTSHIFT, 0)
                            vmouse.write(e.EV_KEY, e.KEY_LEFTALT, 0)
                            gesture_triggered = True

                        # Swipe Left (Alt+Shift+Tab)
                        elif gesture_acc_x < -GESTURE_DISTANCE_THRESHOLD:
                            vmouse.write(e.EV_KEY, e.KEY_LEFTALT, 1)
                            vmouse.write(e.EV_KEY, e.KEY_TAB, 1)
                            vmouse.syn()
                            time.sleep(0.05)
                            vmouse.write(e.EV_KEY, e.KEY_TAB, 0)
                            vmouse.write(e.EV_KEY, e.KEY_LEFTALT, 0)
                            gesture_triggered = True

                        # Swipe Up (Overview/Super)
                        elif gesture_acc_y < -GESTURE_DISTANCE_THRESHOLD:
                            vmouse.write(e.EV_KEY, e.KEY_LEFTMETA, 1)
                            vmouse.syn()
                            time.sleep(0.05)
                            vmouse.write(e.EV_KEY, e.KEY_LEFTMETA, 0)
                            gesture_triggered = True
                        
                        # Swipe Down (Show Desktop / Super+D)
                        elif gesture_acc_y > GESTURE_DISTANCE_THRESHOLD:
                            vmouse.write(e.EV_KEY, e.KEY_LEFTMETA, 1)
                            vmouse.write(e.EV_KEY, e.KEY_D, 1)
                            vmouse.syn()
                            time.sleep(0.05)
                            vmouse.write(e.EV_KEY, e.KEY_D, 0)
                            vmouse.write(e.EV_KEY, e.KEY_LEFTMETA, 0)
                            gesture_triggered = True

                    # --- 2 FINGER SCROLLING ---
                    elif current_finger_count == 2:
                        is_scrolling = True
                        scroll_acc_y += dy
                        scroll_acc_x += dx
                        direction = 1 if NATURAL_SCROLLING else -1

                        # Vertical Scroll
                        if abs(scroll_acc_y) > SCROLL_DIVIDER:
                            ticks = int(scroll_acc_y / SCROLL_DIVIDER)
                            vmouse.write(e.EV_REL, e.REL_WHEEL, ticks * direction)
                            scroll_acc_y -= (ticks * SCROLL_DIVIDER)
                            last_scroll_time = now
                        
                        # Horizontal Scroll (New)
                        # Standard convention: +REL_HWHEEL is scroll right.
                        # Natural: Move finger Right (dx > 0) -> Content Left -> View Right (Scroll Left)
                        # So if Natural=True, dx(+) should result in REL_HWHEEL(-)
                        if abs(scroll_acc_x) > SCROLL_DIVIDER:
                            ticks = int(scroll_acc_x / SCROLL_DIVIDER)
                            # Invert X logic for natural feel relative to vertical
                            h_direction = -direction 
                            vmouse.write(e.EV_REL, e.REL_HWHEEL, ticks * h_direction)
                            scroll_acc_x -= (ticks * SCROLL_DIVIDER)
                            last_scroll_time = now

                    # --- 1 FINGER MOVEMENT ---
                    elif current_finger_count == 1 and not is_scrolling and not gesture_triggered:
                        curr_p = slots[0]['p']
                        move_dist = abs(dx) + abs(dy)
                        
                        if curr_p < MIN_MOVE_PRESSURE: pass 
                        elif curr_p < LOW_PRESSURE_THRESHOLD and move_dist < SMALL_MOVE_CUTOFF: pass 
                        
                        elif abs(dx) < 400 and abs(dy) < 400:
                            speed = abs(dx) + abs(dy)
                            accel = ACCEL_FACTOR if speed > 15 else 1.0
                            vmouse.write(e.EV_REL, e.REL_X, int(dx * MOVE_SENSITIVITY * accel))
                            vmouse.write(e.EV_REL, e.REL_Y, int(dy * MOVE_SENSITIVITY * accel))

                vmouse.syn()
                prev_slots = {s: data.copy() for s, data in slots.items()}

if __name__ == "__main__":
    main()
