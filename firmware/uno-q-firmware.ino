// Viam Arduino Uno Q board firmware
// Protocol: ASCII line-based commands over Serial at 115200 baud
// Responses: "OK [value]" or "ERR message"

#define FIRMWARE_VERSION "UNO-Q v1"

// Timer channels for PWM pins: 3(TIM2), 5(TIM2), 6(TIM3), 9(TIM1), 10(TIM1), 11(TIM1)
const int PWM_PINS[] = {3, 5, 6, 9, 10, 11};
const int PWM_PIN_COUNT = 6;

void setup() {
  analogReadResolution(12); // 12-bit ADC: 0-4095
  Serial.begin(115200);
  while (!Serial) { delay(10); }
  Serial.println("OK " FIRMWARE_VERSION);
}

void loop() {
  if (Serial.available()) {
    String cmd = Serial.readStringUntil('\n');
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
    Serial.println("OK " FIRMWARE_VERSION);

  } else if (op == "GET") {
    int pin = rest.toInt();
    pinMode(pin, INPUT);
    Serial.println("OK " + String(digitalRead(pin)));

  } else if (op == "SET") {
    int sp = rest.indexOf(' ');
    int pin = rest.substring(0, sp).toInt();
    int val = rest.substring(sp + 1).toInt();
    pinMode(pin, OUTPUT);
    digitalWrite(pin, val ? HIGH : LOW);
    Serial.println("OK");

  } else if (op == "PWM") {
    int sp = rest.indexOf(' ');
    int pin = rest.substring(0, sp).toInt();
    float duty = rest.substring(sp + 1).toFloat();
    if (!isPWMPin(pin)) { Serial.println("ERR not a PWM pin"); return; }
    // analogWrite expects 0-255; duty is 0.0-1.0
    analogWrite(pin, (int)(duty * 255.0));
    Serial.println("OK");

  } else if (op == "PWMGET") {
    // STM32 does not expose a direct way to read back duty cycle;
    // return ERR to indicate not supported (caller should track state)
    Serial.println("ERR PWMGET not supported on STM32");

  } else if (op == "FREQ") {
    int sp = rest.indexOf(' ');
    int pin = rest.substring(0, sp).toInt();
    int freqHz = rest.substring(sp + 1).toInt();
    if (!isPWMPin(pin)) { Serial.println("ERR not a PWM pin"); return; }
    setTimerFrequency(pin, freqHz);
    Serial.println("OK");

  } else if (op == "FREQGET") {
    Serial.println("ERR FREQGET not supported on STM32");

  } else if (op == "ADC") {
    int channel = rest.toInt();
    if (channel < 0 || channel > 5) { Serial.println("ERR invalid ADC channel"); return; }
    int val = analogRead(A0 + channel);
    Serial.println("OK " + String(val));

  } else {
    Serial.println("ERR unknown command: " + op);
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
