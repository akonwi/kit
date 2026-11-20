# User testing on a mobile device

Read this when the target is an iOS or Android app on a simulator, emulator,
or device. Mechanics live in the device skills:

- Driving the app on iOS or Android: activate `agent-device`, and use the "QA from acceptance criteria" loop in its exploration reference.
- Booting, installing, launching, or configuring an iOS Simulator on Xcode 27 or later (DeviceHub): activate `ios-simulator`.
- An open-ended bug hunt with no acceptance criteria ("dogfood this app"): use `dogfood` instead of this skill.

If none of these skills or tools are available in the session, report the
blocker and stop.

## Setup

- Use the build, simulator or device, and app state named in the brief. Don't build or reinstall the app unless asked; a reinstall can wipe the app's data and login.
- Record the device model, OS version, and app build in the report. Mobile findings often depend on them.
- Set appearance (light or dark), locale, and permissions deterministically before the flow when the criteria depend on them, and restore them afterwards.

## What mobile testing adds

Cover these when they're in scope for the flow:

- **First launch and permissions:** the permission prompt appears at the right moment, and both allow and deny lead somewhere sensible.
- **Keyboard:** the keyboard doesn't cover the field being typed in or the button that submits it, the return key does the expected thing, and the keyboard dismisses.
- **Gestures and navigation:** swipe back, pull to refresh, and the system back button on Android behave like the platform. Use the app's own back control and the system gesture as separate checks.
- **Size and orientation:** small and large screens, rotation if the app supports it, and Dynamic Type or font scaling when text layout is in scope.
- **Interruptions:** backgrounding and returning, and an alert or incoming notification mid-flow, without losing entered data.
- **Deep links:** opening the URL lands on the right screen in the right state, whether or not the app was already running.

## Diagnostics

Collect app logs only around the step being checked, and read them with
`grep` or `tail` rather than dumping whole files. A crash or error logged at
the step where the UI went wrong belongs in the finding.

## Evidence

- Take a screenshot at each checkpoint. For a behavioral bug, record a short repro video if the device tool supports recording, and still capture the key steps as screenshots.
- Name the device and orientation in each caption when more than one was tested.
