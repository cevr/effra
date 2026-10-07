const __ef_cancel = child => Effect.sync(() => child.fiber.interruptUnsafe());
