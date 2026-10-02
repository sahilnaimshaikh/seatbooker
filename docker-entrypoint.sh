#!/bin/sh
set -eu

./seatbooking migrate
exec ./seatbooking serve