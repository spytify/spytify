//go:build darwin && cgo

#import <Foundation/Foundation.h>
#include <stdlib.h>
#include <string.h>

// spytifyTrash moves the file at path to the Trash and sets trashed to where
// it went there. It returns NULL, or why it could not. Both strings are the
// caller's to free.
char *spytifyTrash(const char *path, char **trashed) {
	@autoreleasepool {
		NSString *name = [NSString stringWithUTF8String:path];
		if (name == nil) {
			return strdup("the path is not UTF-8");
		}
		NSURL *result = nil;
		NSError *err = nil;
		if (![[NSFileManager defaultManager] trashItemAtURL:[NSURL fileURLWithPath:name] resultingItemURL:&result error:&err]) {
			const char *why = err.localizedDescription.UTF8String;
			return strdup(why != NULL ? why : "unknown error");
		}
		const char *where = result.path.UTF8String;
		*trashed = strdup(where != NULL ? where : "");
		return NULL;
	}
}
