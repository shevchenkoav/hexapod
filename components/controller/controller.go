package controller

import (
	"io"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/adammck/hexapod"
	"github.com/adammck/hexapod/math3d"
	"github.com/adammck/sixaxis"
)

const (
	moveSpeed           = 100.0
	rotSpeed            = 15.0
	horizontalLookScale = 250.0
	verticalLookScale   = 250.0

	focalHorizontalOffset = 0
	focalVerticalOffset   = 43 + 34.5 // y offset from origin + y distance to middle of lens
	focalDistance         = 500

	// Distance to adjust the clearance each time Up or Down is pressed.
	clearanceStep = 10

	// Minimum pressure needed to trigger a button press.
	minButtonPressure = 10

	// Maximum angle (in degrees) to bank to the left or right using the
	// orientation of the controller.
	bankScale = 15.0

	// Maximum angle (in degrees) to pitch forwards or backwards using the
	// orientation of the controller.
	pitchScale = 15.0

	// TODO: Document what 'offset' is, here and in the legs.
	xOffsetScale = 40.0
	zOffsetScale = 40.0
)

type Controller struct {
	sa *sixaxis.SA

	clearance float64

	// Keep track of whether various buttons were being pressed during the
	// previous tick, to avoid key repeat.
	upLatch    Latch
	downLatch  Latch
	leftLatch  Latch
	rightLatch Latch
	psLatch    Latch

	// Track select + button options, which change states.
	selectTriangle Latch

	// Enable target orientation mode, where the target bank/pitch (x/y) are set
	// using the controller orientation. Press the PS button to toggle. Defaults
	// to false.
	setTargetOrientation bool
}

var log = logrus.WithFields(logrus.Fields{
	"pkg": "controller",
})

func New(r io.Reader) *Controller {
	return &Controller{
		sa:        sixaxis.New(r),
		clearance: 40,
	}
}

func (c *Controller) Boot() error {
	go c.sa.Run()
	return nil
}

func (c *Controller) Tick(now time.Time, state *hexapod.State) error {

	// Do nothing if we're shutting down.
	if state.Shutdown {
		return nil
	}

	// At any time, pressing start shuts down the hex.
	if c.sa.Start && !state.Shutdown {
		log.Warn("Pressed START, shutting down")
		state.Shutdown = true
	}

	// Set the target position and heading (rotation around the plane parallel
	// to the ground) relative to the current pose, such that holding e.g. up on
	// the left stick moves the machine steadily forwards.
	state.Target = state.Pose.Add(math3d.Pose{
		Position: math3d.Vector3{
			X: (float64(c.sa.LeftStick.X) / 127.0) * moveSpeed,
			Z: (float64(-c.sa.LeftStick.Y) / 127.0) * moveSpeed,
		},
		Heading: (float64(c.sa.R2-c.sa.L2) / 127.0) * rotSpeed,
	})

	// Set the target Y position (clearance between chassis and ground)
	// absolutely. We don't want the body to rise continuously.
	state.Target.Position.Y = c.clearance

	// If target orientation mode is enabled, set the target XZ orientation to
	// match the controller. (Note that the axes are different and inverted.)
	if c.setTargetOrientation {
		state.Target.Pitch = -c.sa.Orientation.Y() * pitchScale
		state.Target.Bank = -c.sa.Orientation.X() * bankScale
	} else {
		state.Target.Pitch = 0
		state.Target.Bank = 0
	}

	// Set offset using the right stick while R1 is held down.
	if c.sa.R1 > minButtonPressure {
		state.Offset = math3d.Vector3{
			X: (float64(c.sa.RightStick.X) / 127.0 * xOffsetScale),
			Z: (float64(c.sa.RightStick.Y*-1) / 127.0 * zOffsetScale),
		}
	} else {

		// Use the right stick to set the focal point, which the head aims at. Note
		// that (a) we discard the pitch+bank orientation of the hex pose, so that
		// our focal point is "forwards" relative to the ground rather than the
		// chassis, and (b) that the Y axis is inverted from the pull-down-to-look-
		// up scheme often used in games. This is all very silly, but looks cool.
		fp := state.Pose.Add(math3d.Pose{
			Pitch: -state.Pose.Pitch,
			Bank:  -state.Pose.Bank,
		}).Add(math3d.Pose{
			Position: math3d.Vector3{
				X: (float64(c.sa.RightStick.X) / 127.0 * horizontalLookScale) + focalHorizontalOffset,
				Y: (float64(c.sa.RightStick.Y*-1) / 127.0 * verticalLookScale) + focalVerticalOffset,
				Z: focalDistance,
			},
			Heading: 0,
		}).Position
		state.LookAt = &fp
	}

	// Toggle target orientation mode by pressing PS.
	if c.psLatch.Run(c.sa.PS) {
		c.setTargetOrientation = !c.setTargetOrientation
		log.Infof("setTargetOrientation=%v", c.setTargetOrientation)
	}

	// Increase clearance by pressing Up
	if c.upLatch.Run(c.sa.Up > minButtonPressure) {
		c.clearance += clearanceStep
		log.Infof("clearance=%v", c.clearance)
	}

	// Decrease clearance by pressing Down
	if c.downLatch.Run(c.sa.Down > minButtonPressure) {
		c.clearance -= clearanceStep
		log.Infof("clearance=%v", c.clearance)
	}

	// Increase speed by pressing right
	if c.rightLatch.Run(c.sa.Right > minButtonPressure) {
		state.Speed += 1
		log.Infof("Speed=%v", state.Speed)
	}

	// Decrease speed by pressing left
	if c.leftLatch.Run(c.sa.Left > minButtonPressure) {
		state.Speed -= 1
		log.Infof("Speed=%v", state.Speed)
	}

	// Cycle through gaits by pressing select + triangle
	if c.selectTriangle.Run(c.sa.Select && c.sa.Triangle > minButtonPressure) {
		state.GaitIndex += 1
		log.Infof("GaitIndex=%v", state.GaitIndex)
	}

	return nil
}
