package points

import "gorm.io/gorm"

type PointLedger struct {
	gorm.Model
	UserID        uint    `gorm:"not null;index"`
	OperationKey  *string `gorm:"size:128;uniqueIndex"`
	Change        int     `gorm:"not null"`
	BalanceAfter  uint    `gorm:"not null"`
	Direction     string  `gorm:"size:20;not null;index"`
	Source        string  `gorm:"size:50;not null;index"`
	ReferenceType string  `gorm:"size:50;not null"`
	ReferenceID   uint    `gorm:"not null;default:0"`
	Description   string  `gorm:"size:255;not null"`
}

type PointOperation struct {
	gorm.Model
	UserID       uint   `gorm:"not null;index:idx_point_operations_user_key,unique"`
	OperationKey string `gorm:"size:128;not null;index:idx_point_operations_user_key,unique"`
	Change       int    `gorm:"not null"`
	BalanceAfter uint   `gorm:"not null"`
}

func (PointOperation) TableName() string {
	return "point_operations"
}

type UserCheckIn struct {
	gorm.Model
	UserID      uint   `gorm:"not null;index:idx_user_check_ins_user_date,unique"`
	CheckInDate string `gorm:"size:10;not null;index:idx_user_check_ins_user_date,unique"`
}

type UserPrivilege struct {
	gorm.Model
	UserID       uint   `gorm:"not null;index:idx_user_privileges_user_key,unique"`
	PrivilegeKey string `gorm:"size:50;not null;index:idx_user_privileges_user_key,unique"`
	Cost         uint   `gorm:"not null"`
}
