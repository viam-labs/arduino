// Viam Arduino Uno Q board firmware
// Protocol: ASCII line-based commands over Serial1 at 115200 baud
// Responses: "OK [value]" or "ERR message"
//
// IMPORTANT: On the Arduino Uno Q, Serial1 (D0/D1, PB6/PB7) is the UART
// connected to the Qualcomm Linux SoC via /dev/ttyHS1. Serial (USB CDC)
// goes to the Mac/host and is NOT used here.
//
// Before deploying, stop the arduino-router service on the Linux side:
//   sudo systemctl stop arduino-router
//   sudo systemctl disable arduino-router
// The router owns /dev/ttyHS1 by default; disabling it frees the port.

#define FIRMWARE_VERSION "UNO-Q v1"

// ---- Digital interrupt support ----
#define MAX_INT_SLOTS 8

struct IntSlot {
    int  pin;
    bool active;
    volatile bool triggered;
    volatile bool lastHigh;
};

static IntSlot intSlots[MAX_INT_SLOTS];

#define MAKE_ISR(N) \
static void isr##N() { \
    if (intSlots[N].active) { \
        intSlots[N].triggered = true; \
        intSlots[N].lastHigh = (bool)digitalRead(intSlots[N].pin); \
    } \
}
MAKE_ISR(0) MAKE_ISR(1) MAKE_ISR(2) MAKE_ISR(3)
MAKE_ISR(4) MAKE_ISR(5) MAKE_ISR(6) MAKE_ISR(7)

static void (*isrTable[MAX_INT_SLOTS])() =
    {isr0, isr1, isr2, isr3, isr4, isr5, isr6, isr7};

// PWM-capable pins (mapped to STM32 timers)
// 3(TIM2), 5(TIM2), 6(TIM3), 9(TIM1), 10(TIM1), 11(TIM1)
const int PWM_PINS[] = {3, 5, 6, 9, 10, 11};
const int PWM_PIN_COUNT = 6;

void setup() {
  analogReadResolution(12); // 12-bit ADC: 0-4095
  Serial1.begin(115200);    // D0/D1 UART — connected to Qualcomm ttyHS1
  // No while(!Serial) — Serial1 is a hardware UART, always ready
  Serial1.println("OK " FIRMWARE_VERSION);
}

void loop() {
  // Poll interrupt slots and emit TICK notifications.
  for (int i = 0; i < MAX_INT_SLOTS; i++) {
    if (intSlots[i].active && intSlots[i].triggered) {
      intSlots[i].triggered = false;
      Serial1.println(
        "TICK " + String(intSlots[i].pin) +
        " " + String(intSlots[i].lastHigh ? 1 : 0) +
        " " + String(micros())
      );
    }
  }

  if (Serial1.available()) {
    String cmd = Serial1.readStringUntil('\n');
    cmd.trim();
    if (cmd.length() > 0) {
      handleCommand(cmd);
    }
  }
}

bool isPWMPin(int pin) {
  for (int i = 0; i < PWM_PIN_COUNT; i++) {
    if (PWM_PINS[i] == pin) return true;
  }
  return false;
}

void handleCommand(const String& cmd) {
  int firstSpace = cmd.indexOf(' ');
  String op = (firstSpace == -1) ? cmd : cmd.substring(0, firstSpace);
  String rest = (firstSpace == -1) ? "" : cmd.substring(firstSpace + 1);

  if (op == "HELLO") {
    Serial1.println("OK " FIRMWARE_VERSION);

  } else if (op == "GET") {
    int pin = rest.toInt();
    pinMode(pin, INPUT);
    Serial1.println("OK " + String(digitalRead(pin)));

  } else if (op == "SET") {
    int sp = rest.indexOf(' ');
    int pin = rest.substring(0, sp).toInt();
    int val = rest.substring(sp + 1).toInt();
    pinMode(pin, OUTPUT);
    digitalWrite(pin, val ? HIGH : LOW);
    Serial1.println("OK");

  } else if (op == "PWM") {
    int sp = rest.indexOf(' ');
    int pin = rest.substring(0, sp).toInt();
    float duty = rest.substring(sp + 1).toFloat();
    if (!isPWMPin(pin)) { Serial1.println("ERR not a PWM pin"); return; }
    // analogWrite expects 0-255; duty is 0.0-1.0
    analogWrite(pin, (int)(duty * 255.0));
    Serial1.println("OK");

  } else if (op == "PWMGET") {
    Serial1.println("ERR PWMGET not supported on STM32");

  } else if (op == "FREQ") {
    int sp = rest.indexOf(' ');
    int pin = rest.substring(0, sp).toInt();
    int freqHz = rest.substring(sp + 1).toInt();
    if (!isPWMPin(pin)) { Serial1.println("ERR not a PWM pin"); return; }
    setTimerFrequency(pin, freqHz);
    Serial1.println("OK");

  } else if (op == "FREQGET") {
    Serial1.println("ERR FREQGET not supported on STM32");

  } else if (op == "ADC") {
    int channel = rest.toInt();
    if (channel < 0 || channel > 5) { Serial1.println("ERR invalid ADC channel"); return; }
    int val = analogRead(A0 + channel);
    Serial1.println("OK " + String(val));

  } else if (op == "INT") {
    int sp = rest.indexOf(' ');
    int pin = rest.substring(0, sp).toInt();
    String mode = rest.substring(sp + 1);
    mode.trim();

    // Detach any existing slot for this pin.
    for (int i = 0; i < MAX_INT_SLOTS; i++) {
      if (intSlots[i].active && intSlots[i].pin == pin) {
        detachInterrupt(digitalPinToInterrupt(pin));
        intSlots[i].active = false;
        break;
      }
    }

    if (mode == "NONE") {
      Serial1.println("OK");
      return;
    }

    // Find a free slot.
    int slot = -1;
    for (int i = 0; i < MAX_INT_SLOTS; i++) {
      if (!intSlots[i].active) { slot = i; break; }
    }
    if (slot < 0) {
      Serial1.println("ERR no interrupt slots");
      return;
    }

    int imode = CHANGE;
    if (mode == "RISING")  imode = RISING;
    if (mode == "FALLING") imode = FALLING;

    intSlots[slot] = { pin, true, false, (bool)digitalRead(pin) };
    attachInterrupt(digitalPinToInterrupt(pin), isrTable[slot], imode);
    Serial1.println("OK");

  } else {
    Serial1.println("ERR unknown command: " + op);
  }
}

// setTimerFrequency adjusts the PWM frequency for the given pin
// using direct STM32U585 timer register access.
// Pin-to-timer mapping: 3->TIM2_CH2, 5->TIM2_CH1, 6->TIM3_CH1,
//                        9->TIM1_CH2, 10->TIM1_CH3, 11->TIM1_CH1
void setTimerFrequency(int pin, int freqHz) {
  if (freqHz <= 0) return;
  // 80 MHz is the STM32U585 timer clock after PLL
  const uint32_t timerClock = 80000000UL;
  uint32_t period = timerClock / freqHz - 1;

  TIM_TypeDef* tim = nullptr;
  if (pin == 3 || pin == 5) tim = TIM2;
  else if (pin == 6)        tim = TIM3;
  else if (pin == 9 || pin == 10 || pin == 11) tim = TIM1;

  if (tim != nullptr) {
    tim->PSC = 0;           // no prescaler
    tim->ARR = period;      // auto-reload sets the period
    tim->EGR = TIM_EGR_UG; // update event to apply registers
  }
}
